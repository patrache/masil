package main

// The API test tool (v0.1.3 C3) registers its tool, palette commands, dialogs
// and settings row from init(); it stays hidden while its feature flag is off
// (features.On("api") — on by default in the 0.1.3 release set).
import _ "github.com/patrache/masil/internal/ui/apitool"
