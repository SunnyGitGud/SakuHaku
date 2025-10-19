package main

import (
    "fmt"
    "os"
)

var (
    clientID     string
    clientSecret string
    redirectURI  string
)


func init() {
    // clientID = os.Getenv("ANILIST_CLIENT_ID")
    // clientSecret = os.Getenv("ANILIST_CLIENT_SECRET")
    // redirectURI = os.Getenv("ANILIST_REDIRECT_URI")

	clientID = "31411"
	clientSecret = "MMFE3kY7gHPurCcmGyui0UkoWCNJzNZwidnL6lmG"
	redirectURI = "http://localhost:8888/callback"

    if redirectURI == "" {
        redirectURI = "http://localhost:8888/callback"
    }
    
    if clientID == "" || clientSecret == "" {
        fmt.Println("ERROR: ANILIST_CLIENT_ID and ANILIST_CLIENT_SECRET must be set in .env file")
        os.Exit(1)
    }
}