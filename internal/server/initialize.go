package server

import (
	"strings"

	"github.com/goplus/xgolsw/i18n"
)

// initialize handles the initialize request and sets up the server language preference
func (s *Server) initialize(params *InitializeParams) (*InitializeResult, error) {
	// Set language based on client locale
	s.setLanguageFromLocale(params.Locale)

	// Create server capabilities
	capabilities := ServerCapabilities{
		// TODO(wyvern): Configure server capabilities based on client capabilities
		// For now, return empty capabilities as placeholder
	}

	return &InitializeResult{
		Capabilities: capabilities,
		ServerInfo: &ServerInfo{
			Name:    "XGo Language Server",
			Version: "0.1.0",
		},
	}, nil
}

// setLanguageFromLocale sets the server language based on the client locale
func (s *Server) setLanguageFromLocale(locale string) {
	// Default to English
	s.language = i18n.LanguageEN

	// Check if locale starts with Chinese indicators
	locale = strings.ToLower(locale)
	if strings.HasPrefix(locale, "zh") || strings.HasPrefix(locale, "cn") {
		s.language = i18n.LanguageCN
	}
}

// translate translates a diagnostic message based on the server's current language
func (s *Server) translate(message string) string {
	return i18n.Translate(message, s.language)
}
