package graph

// stackExtractor indexes the tech contract (docs/stack.md) and dependency
// manifests into tech nodes with uses_tech edges. Fully implemented in
// extract_stack_impl.go; this declares the type for defaultExtractors.
type stackExtractor struct{}
