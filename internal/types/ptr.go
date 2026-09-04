package types

// Ptr returns a pointer to v.
//
// Several fields in this package are pointers so that "unset" is
// distinguishable from a meaningful zero value — ChunkDelayMs (0 means "no
// delay"), TurnNumber, StrictArgs, chaos Rate. Constructing those inline
// otherwise needs a temporary variable at every call site, which is why each
// test package had grown its own intPtr helper.
func Ptr[T any](v T) *T { return &v }
