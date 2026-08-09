package handlers

import (
	"net/http"
)

type Router struct {
	Pattern string
	Handler http.HandlerFunc
}
