module github.com/stewartbrothers/gaia

go 1.25.5

// Pinned to a vulnerability-free 1.25 patch release. govulncheck
// (#140 part 3) flagged 7 standard-library CVEs against 1.25.5
// (the floor mcp-go v0.50 forced); 1.25.9 is the latest 1.25.x at
// the time of this commit. Bump the toolchain ahead of CI's
// go-version when a new patch lands to keep govulncheck green.
toolchain go1.25.9

require (
	github.com/mark3labs/mcp-go v0.50.0
	github.com/spf13/cobra v1.10.2
	golang.org/x/term v0.21.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.14.0 // indirect
)
