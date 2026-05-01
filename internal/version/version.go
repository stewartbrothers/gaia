package version

// Set via -ldflags at build time. Defaults match a from-source build with no git info.
var (
	Version = "dev"
	Commit  = "unknown"
)
