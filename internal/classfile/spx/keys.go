package spx

const (
	// ResourceSetKey stores the resolved [ResourceSet] in a snapshot's resource index.
	ResourceSetKey = "spx/resourceSet"

	// ResourceRootKey stores the root directory used to resolve resources.
	ResourceRootKey = "spx/resourceRoot"

	// MainFileKey stores the path to the primary main.spx file, if present.
	MainFileKey = "spx/mainFile"

	// ResourceRefsKey stores collected resource references in the snapshot.
	ResourceRefsKey = "spx/resourceRefs"
)
