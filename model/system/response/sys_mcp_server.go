package response

import "time"

// SysMCPServerView is safe to return to a client: the bearer token is
// represented only by HasBearerToken.
type SysMCPServerView struct {
	ID             uint      `json:"id"`
	Name           string    `json:"name"`
	Transport      string    `json:"transport"`
	EndpointURL    string    `json:"endpoint_url"`
	HasBearerToken bool      `json:"has_bearer_token"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
