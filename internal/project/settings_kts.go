package project

import (
	"strings"

	"github.com/kaeawc/grit/internal/pathutil"
	tskotlin "github.com/kaeawc/grit/internal/treesitter/kotlin"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func parseKotlinSource(src []byte) *sitter.Tree {
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(tskotlin.Language())); err != nil {
		return nil
	}
	return parser.Parse(src, nil)
}

type settingsModel struct {
	Name         string
	Includes     []string
	ModuleDirs   map[string]string
	Repositories []Repository
}

func parseSettingsKTS(body string) settingsModel {
	return parseSettingsKTSWithProperties(body, nil)
}

func parseSettingsKTSWithProperties(body string, gradleProperties map[string]string) settingsModel {
	tree := parseKotlinSource([]byte(body))
	if tree == nil {
		return settingsModel{}
	}
	defer tree.Close()

	src := []byte(body)
	root := tree.RootNode()
	model := settingsModel{}
	for i := uint(0); i < root.NamedChildCount(); i++ {
		child := root.NamedChild(i)
		switch child.Kind() {
		case "assignment":
			if textOf(nodeChildByKind(child, "directly_assignable_expression"), src) == "rootProject.name" {
				model.Name = unquote(textOf(nodeChildByKind(child, "string_literal"), src))
			}
		case "call_expression":
			switch callName(child, src) {
			case "include":
				for _, path := range parseStringArgs(child, src) {
					model.Includes = append(model.Includes, normalizeIncludePath(path))
				}
			case "pluginManagement":
				model.Repositories = append(model.Repositories, parseRepositoryScope(child, src, "plugin", gradleProperties)...)
			case "dependencyResolutionManagement":
				model.Repositories = append(model.Repositories, parseRepositoryScope(child, src, "dependency", gradleProperties)...)
			}
		}
	}
	if len(model.Repositories) == 0 {
		model.Repositories = append(model.Repositories, collectProjectRepositoriesWithOrigin(body, gradleProperties, "settings")...)
	}
	if len(model.Repositories) == 0 {
		model.Repositories = append(model.Repositories, parseRepositoriesBlock(body, "dependency", gradleProperties)...)
	}
	model.Repositories = annotateRepositories(dedupeRepositories(model.Repositories), "settings", 0)
	model.Includes = mergeStrings(nil, model.Includes)
	model.ModuleDirs = parseProjectDirAssignments(body)
	return model
}

// normalizeIncludePath canonicalises a settings include() argument to
// Gradle's `:path` form. Gradle accepts include("foo") and
// include(":foo") interchangeably, but every other layer of grit's
// project model keys modules by the `:foo` form (e.g. project(":foo")
// dependency references resolve against module Path).
func normalizeIncludePath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, ":") {
		return trimmed
	}
	return ":" + trimmed
}

func parseRepositoryScope(node *sitter.Node, src []byte, scope string, gradleProperties map[string]string) []Repository {
	statements := lambdaStatements(node)
	if statements == nil {
		return nil
	}
	var repos []Repository
	for i := uint(0); i < statements.NamedChildCount(); i++ {
		child := statements.NamedChild(i)
		if child.Kind() != "call_expression" || callName(child, src) != "repositories" {
			continue
		}
		repos = append(repos, parseRepositoriesStatements(lambdaStatements(child), src, scope, gradleProperties)...)
	}
	return repos
}

func parseRepositoriesStatements(statements *sitter.Node, src []byte, scope string, gradleProperties map[string]string) []Repository {
	if statements == nil {
		return nil
	}
	var repos []Repository
	for i := uint(0); i < statements.NamedChildCount(); i++ {
		child := statements.NamedChild(i)
		if child.Kind() != "call_expression" {
			continue
		}
		name := callName(child, src)
		switch name {
		case "google", "mavenCentral", "gradlePluginPortal", "jcenter", "mavenLocal":
			repo := namedRepository(name, scope)
			repo = parseRepositoryBody(repo, child, src, gradleProperties)
			repos = append(repos, repo)
		case "maven":
			url := pathutil.EnsureTrailingSlash(firstNonEmpty(parseStringArgs(child, src)...))
			if url == "" {
				url = pathutil.EnsureTrailingSlash(resolveRepositoryURLExpr(textOf(child, src), gradleProperties))
			}
			repo := Repository{
				Name:  url,
				Kind:  "maven",
				URL:   url,
				Scope: scope,
			}
			repo = parseRepositoryBody(repo, child, src, gradleProperties)
			if repo.Name == "" {
				repo.Name = repo.URL
			}
			repos = append(repos, repo)
		case "exclusiveContent":
			if repo, ok := parseExclusiveContent(child, src, scope, gradleProperties); ok {
				repos = append(repos, repo)
			}
		}
	}
	return annotateRepositories(dedupeRepositories(repos), "settings", 0)
}

func parseExclusiveContent(node *sitter.Node, src []byte, scope string, gradleProperties map[string]string) (Repository, bool) {
	statements := lambdaStatements(node)
	if statements == nil {
		return Repository{}, false
	}
	var repo Repository
	var ok bool
	for i := uint(0); i < statements.NamedChildCount(); i++ {
		child := statements.NamedChild(i)
		if child.Kind() != "call_expression" {
			continue
		}
		switch callName(child, src) {
		case "forRepository":
			candidates := parseRepositoriesStatements(lambdaStatements(child), src, scope, gradleProperties)
			if len(candidates) > 0 {
				repo = candidates[0]
				repo.Exclusive = true
				ok = true
			}
		case "filter":
			if ok {
				repo = parseFilterCalls(repo, lambdaStatements(child), src)
			}
		}
	}
	return repo, ok
}

func parseRepositoryBody(repo Repository, node *sitter.Node, src []byte, gradleProperties map[string]string) Repository {
	statements := lambdaStatements(node)
	if statements == nil {
		return repo
	}
	for i := uint(0); i < statements.NamedChildCount(); i++ {
		child := statements.NamedChild(i)
		switch child.Kind() {
		case "assignment":
			if textOf(nodeChildByKind(child, "directly_assignable_expression"), src) == "name" {
				repo.Name = unquote(textOf(nodeChildByKind(child, "string_literal"), src))
			}
			if textOf(nodeChildByKind(child, "directly_assignable_expression"), src) == "url" {
				if url := resolveRepositoryURLExpr(textOf(child, src), gradleProperties); url != "" {
					repo.URL = pathutil.EnsureTrailingSlash(url)
				}
			}
		case "call_expression":
			if callName(child, src) == "content" {
				repo = parseFilterCalls(repo, lambdaStatements(child), src)
			}
		}
	}
	return repo
}

func parseFilterCalls(repo Repository, statements *sitter.Node, src []byte) Repository {
	if statements == nil {
		return repo
	}
	for i := uint(0); i < statements.NamedChildCount(); i++ {
		child := statements.NamedChild(i)
		if child.Kind() != "call_expression" {
			continue
		}
		args := parseStringArgs(child, src)
		switch callName(child, src) {
		case "includeGroup":
			repo.IncludeGroups = mergeStrings(repo.IncludeGroups, args)
		case "includeGroupByRegex":
			repo.IncludeGroupRegex = mergeStrings(repo.IncludeGroupRegex, args)
		case "excludeGroup":
			repo.ExcludeGroups = mergeStrings(repo.ExcludeGroups, args)
		case "excludeGroupByRegex":
			repo.ExcludeGroupRegex = mergeStrings(repo.ExcludeGroupRegex, args)
		case "includeModule":
			if len(args) >= 2 {
				repo.IncludeModules = mergeStrings(repo.IncludeModules, []string{args[0] + ":" + args[1]})
			}
		case "excludeModule":
			if len(args) >= 2 {
				repo.ExcludeModules = mergeStrings(repo.ExcludeModules, []string{args[0] + ":" + args[1]})
			}
		}
	}
	return repo
}

func lambdaStatements(node *sitter.Node) *sitter.Node {
	callSuffix := nodeChildByKind(node, "call_suffix")
	if callSuffix == nil {
		return nil
	}
	annotatedLambda := nodeChildByKind(callSuffix, "annotated_lambda")
	if annotatedLambda == nil {
		return nil
	}
	lambdaLiteral := nodeChildByKind(annotatedLambda, "lambda_literal")
	if lambdaLiteral == nil {
		return nil
	}
	return nodeChildByKind(lambdaLiteral, "statements")
}

func callName(node *sitter.Node, src []byte) string {
	base := baseCallNode(node)
	return textOf(nodeChildByKind(base, "simple_identifier"), src)
}

func parseStringArgs(node *sitter.Node, src []byte) []string {
	callSuffix := nodeChildByKind(baseCallNode(node), "call_suffix")
	if callSuffix == nil {
		return nil
	}
	valueArguments := nodeChildByKind(callSuffix, "value_arguments")
	if valueArguments == nil {
		return nil
	}
	var out []string
	for i := uint(0); i < valueArguments.NamedChildCount(); i++ {
		arg := valueArguments.NamedChild(i)
		if arg.Kind() == "value_argument" {
			if lit := nodeChildByKind(arg, "string_literal"); lit != nil {
				out = append(out, unquote(textOf(lit, src)))
			}
			continue
		}
		if arg.Kind() == "string_literal" {
			out = append(out, unquote(textOf(arg, src)))
		}
	}
	return out
}

func baseCallNode(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	if nodeChildByKind(node, "simple_identifier") != nil {
		return node
	}
	if inner := nodeChildByKind(node, "call_expression"); inner != nil {
		return baseCallNode(inner)
	}
	return node
}

func nodeChildByKind(node *sitter.Node, kind string) *sitter.Node {
	if node == nil {
		return nil
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == kind {
			return child
		}
	}
	return nil
}

func textOf(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return node.Utf8Text(src)
}

func unquote(v string) string {
	return strings.Trim(v, `"`)
}
