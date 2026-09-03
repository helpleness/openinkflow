package response

import "errors"

// ErrUnauthorized indicates that an operation requires an authenticated user.
var ErrUnauthorized = errors.New("authentication is required")

// ErrForbidden indicates that the authenticated user lacks required permission.
var ErrForbidden = errors.New("permission denied")
