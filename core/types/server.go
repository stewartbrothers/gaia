package types

// ServerVersion is the trimmed version info returned by the forge
// instance's version endpoint. Useful for diagnostics and API
// compatibility checks after an upgrade.
type ServerVersion struct {
	Version string `json:"version"`
}
