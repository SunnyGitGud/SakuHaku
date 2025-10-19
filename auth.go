package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pkg/browser"
)

var (
	authURL      = "https://anilist.co/api/v2/oauth/authorize"
	tokenURL     = "https://anilist.co/api/v2/oauth/token"
	callbackPort = "8888"
	tokenFile    = ".anilist_token"
)

//OAuth Messages

type authSuccessMsg struct {
	token    string
	username string
	userID   int
}

type authErrorMsg struct {
	err error
}

type userListMsg []UserAnimeEntry

// OAuth Implementation
func startOAuthFlow() tea.Cmd {
	return func() tea.Msg {
		// Start local server to receive callback
		codeChan := make(chan string, 1)
		errChan := make(chan error, 1)

		server := &http.Server{Addr: ":" + callbackPort}

		http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
			code := r.URL.Query().Get("code")
			if code == "" {
				errChan <- fmt.Errorf("no code received")
				return
			}

			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, "<html><body><h1>Authentication successful!</h1><p>You can close this window and return to the terminal.</p></body></html>")

			codeChan <- code
		})

		go func() {
			if err := server.ListenAndServe(); err != http.ErrServerClosed {
				errChan <- err
			}
		}()

		// Open browser for authentication
		authURLWithParams := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code",
			authURL, clientID, url.QueryEscape(redirectURI))

		if err := browser.OpenURL(authURLWithParams); err != nil {
			return authErrorMsg{err: err}
		}

		// Wait for callback with timeout
		select {
		case code := <-codeChan:
			server.Shutdown(context.Background())

			// Exchange code for token
			token, err := exchangeCodeForToken(code)
			if err != nil {
				return authErrorMsg{err: err}
			}

			// Get user info
			username, userID, err := getUserInfo(token)
			if err != nil {
				return authErrorMsg{err: err}
			}

			return authSuccessMsg{
				token:    token,
				username: username,
				userID:   userID,
			}

		case err := <-errChan:
			server.Shutdown(context.Background())
			return authErrorMsg{err: err}

		case <-time.After(5 * time.Minute):
			server.Shutdown(context.Background())
			return authErrorMsg{err: fmt.Errorf("authentication timeout")}
		}
	}
}

func exchangeCodeForToken(code string) (string, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
		"code":          {code},
	}

	resp, err := http.PostForm(tokenURL, data)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.AccessToken == "" {
		return "", fmt.Errorf("no access token in response")
	}

	return result.AccessToken, nil
}

func getUserInfo(token string) (string, int, error) {
	query := `
	query {
		Viewer {
			id
			name
		}
	}
	`

	result, err := makeAuthenticatedRequest(token, query, nil)
	if err != nil {
		return "", 0, err
	}

	return result.Data.Viewer.Name, result.Data.Viewer.ID, nil
}

func fetchUserAnimeList(token string, userID int, status string) tea.Cmd {
	return func() tea.Msg {
		query := `
		query ($userId: Int, $status: MediaListStatus) {
			MediaListCollection(userId: $userId, type: ANIME, status: $status, sort: UPDATED_TIME_DESC) {
				lists {
					name
					entries {
						id
						status
						progress
						score
						updatedAt
						media {
							id
							title {
								romaji
								english
							}
							format
							status
							episodes
							averageScore
							season
							seasonYear
							coverImage {
								large
							}
							siteUrl
						}
					}
				}
			}
		}
		`

		variables := map[string]interface{}{
			"userId": userID,
			"status": status,
		}

		result, err := makeAuthenticatedRequest(token, query, variables)
		if err != nil {
			return userListMsg(nil)
		}

		var entries []UserAnimeEntry
		for _, list := range result.Data.MediaListCollection.Lists {
			entries = append(entries, list.Entries...)
		}

		return userListMsg(entries)
	}
}

func makeAuthenticatedRequest(token, query string, variables map[string]interface{}) (*AniListResponse, error) {
	requestBody := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", "https://graphql.anilist.co", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result AniListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}
