package codemogger

import (
	"path/filepath"
	"strings"
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type languageConfig struct {
	Name          string
	Extensions    []string
	TopLevelNodes map[string]struct{}
	SplitNodes    map[string]struct{}
	Language      func() *sitter.Language
}

func cachedLanguage(create func() *sitter.Language) func() *sitter.Language {
	var once sync.Once
	var lang *sitter.Language
	return func() *sitter.Language {
		once.Do(func() {
			lang = create()
		})
		return lang
	}
}

func setOf(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

var languageConfigs = []*languageConfig{
	{
		Name:          "rust",
		Extensions:    []string{".rs"},
		TopLevelNodes: setOf("function_item", "struct_item", "enum_item", "impl_item", "trait_item", "type_item", "const_item", "static_item", "macro_definition", "mod_item"),
		SplitNodes:    setOf("impl_item", "trait_item", "mod_item"),
		Language:      cachedLanguage(func() *sitter.Language { return sitter.NewLanguage(tree_sitter_rust.Language()) }),
	},
	{
		Name:          "javascript",
		Extensions:    []string{".js", ".jsx", ".mjs", ".cjs"},
		TopLevelNodes: setOf("function_declaration", "generator_function_declaration", "class_declaration", "variable_declaration", "lexical_declaration", "export_statement"),
		SplitNodes:    setOf("class_declaration"),
		Language:      cachedLanguage(func() *sitter.Language { return sitter.NewLanguage(tree_sitter_javascript.Language()) }),
	},
	{
		Name:          "typescript",
		Extensions:    []string{".ts", ".mts", ".cts"},
		TopLevelNodes: setOf("function_declaration", "generator_function_declaration", "class_declaration", "abstract_class_declaration", "interface_declaration", "type_alias_declaration", "enum_declaration", "variable_declaration", "lexical_declaration", "export_statement"),
		SplitNodes:    setOf("class_declaration", "abstract_class_declaration", "interface_declaration"),
		Language:      cachedLanguage(func() *sitter.Language { return sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript()) }),
	},
	{
		Name:          "tsx",
		Extensions:    []string{".tsx"},
		TopLevelNodes: setOf("function_declaration", "generator_function_declaration", "class_declaration", "abstract_class_declaration", "interface_declaration", "type_alias_declaration", "enum_declaration", "variable_declaration", "lexical_declaration", "export_statement"),
		SplitNodes:    setOf("class_declaration", "abstract_class_declaration", "interface_declaration"),
		Language:      cachedLanguage(func() *sitter.Language { return sitter.NewLanguage(tree_sitter_typescript.LanguageTSX()) }),
	},
	{
		Name:          "c",
		Extensions:    []string{".c", ".h"},
		TopLevelNodes: setOf("function_definition", "declaration", "type_definition", "enum_specifier", "struct_specifier", "preproc_def", "preproc_function_def"),
		SplitNodes:    setOf(),
		Language:      cachedLanguage(func() *sitter.Language { return sitter.NewLanguage(tree_sitter_c.Language()) }),
	},
	{
		Name:          "cpp",
		Extensions:    []string{".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx"},
		TopLevelNodes: setOf("function_definition", "class_specifier", "struct_specifier", "enum_specifier", "namespace_definition", "template_declaration", "declaration"),
		SplitNodes:    setOf("class_specifier", "struct_specifier", "namespace_definition"),
		Language:      cachedLanguage(func() *sitter.Language { return sitter.NewLanguage(tree_sitter_cpp.Language()) }),
	},
	{
		Name:          "python",
		Extensions:    []string{".py", ".pyi"},
		TopLevelNodes: setOf("function_definition", "class_definition", "decorated_definition"),
		SplitNodes:    setOf("class_definition"),
		Language:      cachedLanguage(func() *sitter.Language { return sitter.NewLanguage(tree_sitter_python.Language()) }),
	},
	{
		Name:          "go",
		Extensions:    []string{".go"},
		TopLevelNodes: setOf("function_declaration", "method_declaration", "type_declaration", "const_declaration", "var_declaration"),
		SplitNodes:    setOf(),
		Language:      cachedLanguage(func() *sitter.Language { return sitter.NewLanguage(tree_sitter_go.Language()) }),
	},
	{
		Name:          "java",
		Extensions:    []string{".java"},
		TopLevelNodes: setOf("class_declaration", "interface_declaration", "enum_declaration", "record_declaration"),
		SplitNodes:    setOf("class_declaration", "interface_declaration", "enum_declaration"),
		Language:      cachedLanguage(func() *sitter.Language { return sitter.NewLanguage(tree_sitter_java.Language()) }),
	},
	{
		Name:          "php",
		Extensions:    []string{".php"},
		TopLevelNodes: setOf("class_declaration", "interface_declaration", "trait_declaration", "function_definition", "enum_declaration"),
		SplitNodes:    setOf("class_declaration", "interface_declaration", "trait_declaration"),
		Language:      cachedLanguage(func() *sitter.Language { return sitter.NewLanguage(tree_sitter_php.LanguagePHPOnly()) }),
	},
	{
		Name:          "ruby",
		Extensions:    []string{".rb"},
		TopLevelNodes: setOf("module", "class", "method", "singleton_method", "assignment"),
		SplitNodes:    setOf("module", "class"),
		Language:      cachedLanguage(func() *sitter.Language { return sitter.NewLanguage(tree_sitter_ruby.Language()) }),
	},
}

var extMap = func() map[string]*languageConfig {
	out := make(map[string]*languageConfig)
	for _, lang := range languageConfigs {
		for _, ext := range lang.Extensions {
			out[ext] = lang
		}
	}
	return out
}()

func detectLanguage(filePath string) *languageConfig {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == "" {
		return nil
	}
	return extMap[ext]
}

func SupportedLanguages() []string {
	out := make([]string, 0, len(languageConfigs))
	for _, lang := range languageConfigs {
		out = append(out, lang.Name)
	}
	return out
}
