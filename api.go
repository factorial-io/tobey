package main

type SiteRequest struct {
	URL string `json:"url"` // The root URL
}

type SiteResponse struct {
	ID  string `json:"id"`
	URL string `json:"url"` // The root URL
}
