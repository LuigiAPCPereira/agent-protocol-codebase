package graph

type Node struct {
	ID          string
	Kind        string
	Name        string
	Path        string
	Language    string
	PackagePath string
	StartLine   int
	EndLine     int
	External    bool
}

type Edge struct {
	From       string
	To         string
	Relation   string
	Evidence   string
	Resolution string
	Extractor  string
	SourcePath string
	StartLine  int
}

type Result struct {
	Detected bool
	Nodes    []Node
	Edges    []Edge
	Warnings []string
}
