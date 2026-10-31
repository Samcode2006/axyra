package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/axyra/control-plane/internal/models"
	"github.com/golang-jwt/jwt/v5"
)

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Username == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "Username and password are required")
		return
	}

	// For demo/development, allow admin login
	// In production, verify against password hash table
	claims := jwt.MapClaims{
		"sub":     req.Username,
		"org_id":  "org_demo",
		"role":    "admin",
		"iss":     "axyra-control-plane",
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
		"iat":     time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(h.cfg.JWTSecret))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to sign token")
		return
	}

	respondJSON(w, http.StatusOK, models.LoginResponse{Token: tokenString})
}

