package codemogger

import (
	"fmt"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

const maxChunkLines = 150

var bodyWrappers = setOf("class_body", "declaration_list", "field_declaration_list", "body_statement", "block")

func chunkFile(filePath, content, fileHash string, cfg *languageConfig) ([]CodeChunk, error) {
	parser := sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(cfg.Language()); err != nil {
		return nil, fmt.Errorf("set language: %w", err)
	}

	source := []byte(content)
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, nil
	}
	defer tree.Close()

	lines := strings.Split(content, "\n")
	chunks := make([]CodeChunk, 0)

	makeChunk := func(node *sitter.Node, kind string) CodeChunk {
		startLine := int(node.StartPosition().Row) + 1
		endLine := int(node.EndPosition().Row) + 1
		return CodeChunk{
			ChunkKey:  fmt.Sprintf("%s:%d:%d", filePath, startLine, endLine),
			FilePath:  filePath,
			Language:  cfg.Name,
			Kind:      kind,
			Name:      extractName(node, source),
			Signature: extractSignature(node, lines),
			Snippet:   node.Utf8Text(source),
			StartLine: startLine,
			EndLine:   endLine,
			FileHash:  fileHash,
		}
	}

	var splitLargeNode func(node, outerNode *sitter.Node)
	processNode := func(node *sitter.Node) {
		if node == nil {
			return
		}
		switch node.Kind() {
		case "export_statement":
			inner := unwrapExport(node)
			if inner != nil {
				if _, ok := cfg.TopLevelNodes[inner.Kind()]; ok {
					kind := nodeKind(inner.Kind())
					lineCount := int(node.EndPosition().Row-node.StartPosition().Row) + 1
					if lineCount <= maxChunkLines {
						chunks = append(chunks, makeChunk(node, kind))
					} else if _, ok := cfg.SplitNodes[inner.Kind()]; ok {
						splitLargeNode(inner, node)
					} else {
						chunks = append(chunks, makeChunk(node, kind))
					}
					return
				}
				if strings.Contains(inner.Kind(), "function") || strings.Contains(inner.Kind(), "class") {
					chunks = append(chunks, makeChunk(node, nodeKind(inner.Kind())))
				}
			}
			return
		case "decorated_definition":
			inner := node.ChildByFieldName("definition")
			if inner != nil {
				kind := nodeKind(inner.Kind())
				lineCount := int(node.EndPosition().Row-node.StartPosition().Row) + 1
				if lineCount <= maxChunkLines {
					chunks = append(chunks, makeChunk(node, kind))
				} else if _, ok := cfg.SplitNodes[inner.Kind()]; ok {
					splitLargeNode(inner, node)
				} else {
					chunks = append(chunks, makeChunk(node, kind))
				}
				return
			}
		case "template_declaration":
			inner := firstNamedChildNotOfKinds(node, "template_parameter_list")
			if inner != nil {
				kind := nodeKind(inner.Kind())
				lineCount := int(node.EndPosition().Row-node.StartPosition().Row) + 1
				if lineCount <= maxChunkLines {
					chunks = append(chunks, makeChunk(node, kind))
				} else if _, ok := cfg.SplitNodes[inner.Kind()]; ok {
					splitLargeNode(inner, node)
				} else {
					chunks = append(chunks, makeChunk(node, kind))
				}
				return
			}
		}

		if _, ok := cfg.TopLevelNodes[node.Kind()]; !ok {
			return
		}

		lineCount := int(node.EndPosition().Row-node.StartPosition().Row) + 1
		kind := nodeKind(node.Kind())
		if lineCount <= maxChunkLines {
			chunks = append(chunks, makeChunk(node, kind))
			return
		}
		if _, ok := cfg.SplitNodes[node.Kind()]; !ok {
			chunks = append(chunks, makeChunk(node, kind))
			return
		}
		splitLargeNode(node, node)
	}

	splitLargeNode = func(node, outerNode *sitter.Node) {
		hasSubItems := false

		isSubItem := func(kind string) bool {
			if _, ok := cfg.TopLevelNodes[kind]; ok {
				return true
			}
			return strings.Contains(kind, "function") || strings.Contains(kind, "method") || strings.Contains(kind, "constructor")
		}

		for _, child := range namedChildren(node) {
			if isSubItem(child.Kind()) {
				c := child
				chunks = append(chunks, makeChunk(&c, nodeKind(child.Kind())))
				hasSubItems = true
				continue
			}
			if _, ok := bodyWrappers[child.Kind()]; ok {
				cc := child
				for _, inner := range namedChildren(&cc) {
					if isSubItem(inner.Kind()) {
						i := inner
						chunks = append(chunks, makeChunk(&i, nodeKind(inner.Kind())))
						hasSubItems = true
					}
				}
			}
		}

		if !hasSubItems {
			chunks = append(chunks, makeChunk(outerNode, nodeKind(node.Kind())))
		}
	}

	root := tree.RootNode()
	for _, child := range namedChildren(root) {
		c := child
		processNode(&c)
	}

	return chunks, nil
}

func namedChildren(node *sitter.Node) []sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	return node.NamedChildren(cursor)
}

func firstNamedChildNotOfKinds(node *sitter.Node, excluded ...string) *sitter.Node {
	deny := setOf(excluded...)
	for _, child := range namedChildren(node) {
		if _, ok := deny[child.Kind()]; ok {
			continue
		}
		c := child
		return &c
	}
	return nil
}

func unwrapExport(node *sitter.Node) *sitter.Node {
	if node.Kind() != "export_statement" {
		return nil
	}
	for _, child := range namedChildren(node) {
		if child.Kind() == "decorator" || child.Kind() == "comment" {
			continue
		}
		c := child
		return &c
	}
	return nil
}

func extractSignature(node *sitter.Node, lines []string) string {
	row := int(node.StartPosition().Row)
	if row < 0 || row >= len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[row])
}

func extractName(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "export_statement":
		return extractName(unwrapExport(node), source)
	case "decorated_definition":
		return extractName(node.ChildByFieldName("definition"), source)
	case "template_declaration":
		return extractName(firstNamedChildNotOfKinds(node, "template_parameter_list"), source)
	case "singleton_method":
		obj := node.ChildByFieldName("object")
		nameNode := node.ChildByFieldName("name")
		if obj != nil && nameNode != nil {
			return obj.Utf8Text(source) + "." + nameNode.Utf8Text(source)
		}
		if nameNode != nil {
			return nameNode.Utf8Text(source)
		}
	case "assignment":
		children := namedChildren(node)
		if len(children) > 0 {
			return children[0].Utf8Text(source)
		}
	case "function_definition":
		declarator := node.ChildByFieldName("declarator")
		if declarator != nil && declarator.Kind() == "function_declarator" {
			fnName := declarator.ChildByFieldName("declarator")
			if fnName != nil {
				return fnName.Utf8Text(source)
			}
		}
	case "type_definition":
		for _, child := range namedChildren(node) {
			if child.Kind() == "type_identifier" {
				return child.Utf8Text(source)
			}
		}
	case "method_declaration":
		nameNode := node.ChildByFieldName("name")
		receiver := node.ChildByFieldName("receiver")
		if nameNode != nil && receiver != nil {
			children := namedChildren(receiver)
			if len(children) > 0 {
				paramType := children[0].ChildByFieldName("type")
				if paramType != nil {
					return paramType.Utf8Text(source) + "." + nameNode.Utf8Text(source)
				}
			}
		}
		if nameNode != nil {
			return nameNode.Utf8Text(source)
		}
	case "type_declaration":
		for _, child := range namedChildren(node) {
			if child.Kind() == "type_spec" {
				c := child
				nameNode := c.ChildByFieldName("name")
				if nameNode != nil {
					return nameNode.Utf8Text(source)
				}
			}
		}
	case "const_declaration", "var_declaration":
		for _, child := range namedChildren(node) {
			if child.Kind() == "const_spec" || child.Kind() == "var_spec" {
				c := child
				nameNode := c.ChildByFieldName("name")
				if nameNode != nil {
					return nameNode.Utf8Text(source)
				}
			}
		}
	case "variable_declaration":
		for _, child := range namedChildren(node) {
			if child.Kind() == "identifier" {
				return child.Utf8Text(source)
			}
		}
	}

	for _, field := range []string{"name", "identifier", "type_identifier"} {
		child := node.ChildByFieldName(field)
		if child != nil {
			return child.Utf8Text(source)
		}
	}

	typeNode := node.ChildByFieldName("type")
	if typeNode != nil {
		traitNode := node.ChildByFieldName("trait")
		if traitNode != nil {
			return traitNode.Utf8Text(source) + " for " + typeNode.Utf8Text(source)
		}
		return typeNode.Utf8Text(source)
	}

	if node.Kind() == "lexical_declaration" {
		for _, child := range namedChildren(node) {
			if child.Kind() == "variable_declarator" {
				c := child
				nameNode := c.ChildByFieldName("name")
				if nameNode != nil {
					return nameNode.Utf8Text(source)
				}
			}
		}
	}

	return ""
}

func nodeKind(kind string) string {
	switch {
	case strings.Contains(kind, "function") || kind == "function_item":
		return "function"
	case strings.Contains(kind, "struct"):
		return "struct"
	case strings.Contains(kind, "enum"):
		return "enum"
	case strings.Contains(kind, "impl"):
		return "impl"
	case strings.Contains(kind, "trait"):
		return "trait"
	case kind == "type_item" || kind == "type_alias_declaration" || kind == "type_definition" || kind == "type_declaration":
		return "type"
	case strings.Contains(kind, "const"):
		return "const"
	case strings.Contains(kind, "static"):
		return "static"
	case strings.Contains(kind, "macro") || kind == "preproc_def" || kind == "preproc_function_def":
		return "macro"
	case kind == "namespace_definition":
		return "namespace"
	case kind == "template_declaration":
		return "template"
	case strings.Contains(kind, "mod"):
		return "module"
	case strings.Contains(kind, "class"):
		return "class"
	case kind == "method_declaration" || strings.Contains(kind, "method"):
		return "method"
	case strings.Contains(kind, "interface"):
		return "interface"
	case kind == "variable_declaration" || kind == "lexical_declaration" || kind == "var_declaration" || kind == "val_definition" || kind == "assignment":
		return "variable"
	case kind == "declaration":
		return "declaration"
	case kind == "decorated_definition":
		return "function"
	case kind == "test_declaration":
		return "test"
	case kind == "object_definition":
		return "object"
	case kind == "record_declaration":
		return "record"
	case kind == "constructor_declaration":
		return "constructor"
	default:
		return kind
	}
}
