package main

type APIRequest struct {
	URL string `json:"url"` // The root URL
}

type APIResponse struct {
	Site *Site  `json:"site"`
	URL  string `json:"url"` // The root URL
}
