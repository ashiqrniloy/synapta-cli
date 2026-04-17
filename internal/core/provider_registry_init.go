package core

// provider_registry_init.go wires the built-in provider descriptors into the
// package-level ProviderRegistry consumed by ChatService.
//
// To add a new provider:
//  1. Create internal/llm/providers/<name>/descriptor.go implementing
//     llm.ProviderDescriptor (ResolveToken, RefreshToken, Build, and
//     optionally DiscoverModels).
//  2. Add one line below: builtinProviderRegistry.Register(yourpkg.Descriptor())
//
// No other files need to change.

import (
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
	kiloprovider "github.com/ashiqrniloy/synapta-cli/internal/llm/providers/kilo"
	copilotprovider "github.com/ashiqrniloy/synapta-cli/internal/llm/providers/copilot"
)

// builtinProviderRegistry is the package-level registry shared by all
// ChatService instances.  It is populated once during package initialisation.
var builtinProviderRegistry = func() *llm.ProviderRegistry {
	r := llm.NewProviderRegistry()
	r.Register(kiloprovider.Descriptor())
	r.Register(copilotprovider.Descriptor())
	return r
}()
