// Package forges blank-imports every forge adapter so their init()
// registrations populate the provider registry (#309). It is the single
// place that names the concrete forge packages: importing it for its
// side effect makes every registered forge available to
// provider.Build, while keeping internal/forgebuilder — and any other
// consumer — free of direct forgejo/github imports.
//
// Adding a forge is therefore purely additive: write core/<forge> with
// an init() that calls provider.Register, then add one blank import
// line here. No dispatch switch to edit.
package forges

import (
	_ "github.com/stewartbrothers/gaia/core/forgejo"
	_ "github.com/stewartbrothers/gaia/core/github"
)
