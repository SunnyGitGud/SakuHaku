package main

import (
	"encoding/json"
	"fmt"
	"os"
)

//toeken presistence

type savedToken struct {
	AccessToken string `json:"access_token"`
	Username    string `json:"username"`
	UserID      int    `json:"user_id"`
}

func saveToken(token, username string, userID int) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	tokenPath := fmt.Sprintf("%s/%s", homeDir, tokenFile)

	data := savedToken{
		AccessToken: token,
		Username:    username,
		UserID:      userID,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return os.WriteFile(tokenPath, jsonData, 0600)
}

func loadSavedToken() (string, string, int, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", 0, err
	}

	tokenPath := fmt.Sprintf("%s/%s", homeDir, tokenFile)

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", "", 0, err
	}

	var saved savedToken
	if err := json.Unmarshal(data, &saved); err != nil {
		return "", "", 0, err
	}

	_, _, err = getUserInfo(saved.AccessToken)
	if err != nil {
		os.Remove(tokenPath)
		return "", "", 0, fmt.Errorf("token expired")
	}

	// Token is valid, return the saved data
	return saved.AccessToken, saved.Username, saved.UserID, nil
}
