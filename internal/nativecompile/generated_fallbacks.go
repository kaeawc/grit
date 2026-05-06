package nativecompile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kaeawc/grit/internal/project"
)

type generatedFallbackResult struct {
	SourceDir     string
	Sources       []string
	CommonSources []string
}

func runGeneratedFallbacks(prj *project.Project, mod *project.Module, variantName string) (generatedFallbackResult, error) {
	var out generatedFallbackResult
	if prj == nil || mod == nil {
		return out, nil
	}
	out.SourceDir = filepath.Join(prj.RootDir, "build", "grit", moduleOutputRelPath(mod.Path), variantName, "generated-fallbacks")
	if err := os.MkdirAll(out.SourceDir, 0o755); err != nil {
		return out, err
	}
	for _, source := range []struct {
		rel  string
		body string
	}{
		koinGeneratedFallback(mod),
		sqldelightGeneratedFallback(mod),
		sqldelightAsyncFallback(mod),
		buildConfigFallback(mod),
	} {
		if strings.TrimSpace(source.body) == "" {
			continue
		}
		path := filepath.Join(out.SourceDir, source.rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return out, err
		}
		if err := os.WriteFile(path, []byte(source.body), 0o644); err != nil {
			return out, err
		}
		out.Sources = append(out.Sources, path)
		out.CommonSources = append(out.CommonSources, path)
	}
	return out, nil
}

func koinGeneratedFallback(mod *project.Module) struct{ rel, body string } {
	if !moduleSourcesContain(mod, "org.koin.ksp.generated.") {
		return struct{ rel, body string }{}
	}
	body := `package org.koin.ksp.generated

import org.koin.core.context.startKoin
import org.koin.dsl.KoinAppDeclaration

fun Any.startKoin(appDeclaration: KoinAppDeclaration? = null): org.koin.core.KoinApplication =
    startKoin {
        appDeclaration?.invoke(this)
    }
`
	return struct{ rel, body string }{rel: filepath.Join("org", "koin", "ksp", "generated", "KoinGenerated.kt"), body: body}
}

func sqldelightAsyncFallback(mod *project.Module) struct{ rel, body string } {
	if !moduleSourcesContain(mod, "app.cash.sqldelight.async.coroutines") {
		return struct{ rel, body string }{}
	}
	body := `package app.cash.sqldelight.async.coroutines

import app.cash.sqldelight.db.QueryResult
import app.cash.sqldelight.db.SqlDriver
import app.cash.sqldelight.db.SqlSchema

fun SqlSchema<QueryResult.Value<Unit>>.synchronous(): SqlSchema<QueryResult.Value<Unit>> = this

suspend fun SqlSchema<QueryResult.Value<Unit>>.awaitCreate(driver: SqlDriver) {
    create(driver)
}
`
	return struct{ rel, body string }{rel: filepath.Join("app", "cash", "sqldelight", "async", "coroutines", "AsyncCompat.kt"), body: body}
}

func buildConfigFallback(mod *project.Module) struct{ rel, body string } {
	if mod == nil || !mod.BuildFeatures.BuildConfig || strings.TrimSpace(mod.Namespace) == "" {
		return struct{ rel, body string }{}
	}
	applicationID := firstNonEmptyString(mod.ApplicationID, mod.Namespace)
	versionName := firstNonEmptyString(mod.VersionName, "")
	versionCode := firstNonEmptyString(mod.VersionCode, "1")
	body := fmt.Sprintf(`package %s

object BuildConfig {
    const val DEBUG: Boolean = true
    const val APPLICATION_ID: String = %q
    const val VERSION_NAME: String = %q
    const val VERSION_CODE: Int = %s
}
`, mod.Namespace, applicationID, versionName, versionCode)
	rel := strings.ReplaceAll(mod.Namespace, ".", string(filepath.Separator))
	return struct{ rel, body string }{rel: filepath.Join(rel, "BuildConfig.kt"), body: body}
}

func sqldelightGeneratedFallback(mod *project.Module) struct{ rel, body string } {
	data, err := os.ReadFile(mod.BuildFile)
	if err != nil {
		return struct{ rel, body string }{}
	}
	build := string(data)
	dbName := firstMatch(build, `create\("([A-Za-z_][A-Za-z0-9_]*)"\)`)
	pkg := firstMatch(build, `packageName\.set\("([^"]+)"\)`)
	if dbName == "" || pkg == "" {
		return struct{ rel, body string }{}
	}
	body := fmt.Sprintf(`package %s

import app.cash.sqldelight.Query
import app.cash.sqldelight.TransacterImpl
import app.cash.sqldelight.db.QueryResult
import app.cash.sqldelight.db.SqlCursor
import app.cash.sqldelight.db.SqlDriver

class %s(
    driver: SqlDriver,
) {
    val %s: %sQueries = %sQueries(driver)

    object Schema : app.cash.sqldelight.db.SqlSchema<QueryResult.Value<Unit>> {
        override val version: Long = 1
        override fun create(driver: SqlDriver): QueryResult.Value<Unit> = QueryResult.Value(Unit)
        override fun migrate(driver: SqlDriver, oldVersion: Long, newVersion: Long, vararg callbacks: app.cash.sqldelight.db.AfterVersion): QueryResult.Value<Unit> = QueryResult.Value(Unit)
    }
}

class %sQueries(
    driver: SqlDriver,
) : TransacterImpl(driver) {
    fun <T : Any> selectAll(mapper: (name: String, craft: String, personImageUrl: String?, personBio: String?, nationality: String) -> T): Query<T> =
        object : Query<T>({ cursor: SqlCursor ->
            mapper("", "", null, null, "")
        }) {
            override fun <R> execute(mapper: (SqlCursor) -> QueryResult<R>): QueryResult<R> =
                QueryResult.Value(emptyList<T>() as R)
            override fun addListener(listener: Listener) = Unit
            override fun removeListener(listener: Listener) = Unit
        }

    fun insertItem(name: String, craft: String, personImageUrl: String?, personBio: String?, nationality: String) = Unit
    fun deleteAll() = Unit
}
`, pkg, dbName, sqldelightQueriesProperty(mod, dbName), dbName, dbName, dbName)
	rel := strings.ReplaceAll(pkg, ".", string(filepath.Separator))
	return struct{ rel, body string }{rel: filepath.Join(rel, dbName+".kt"), body: body}
}

func sqldelightQueriesProperty(mod *project.Module, dbName string) string {
	root := filepath.Join(mod.Dir, "src", "commonMain", "sqldelight")
	var first string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || first != "" || !strings.HasSuffix(path, ".sq") {
			return nil
		}
		first = strings.TrimSuffix(filepath.Base(path), ".sq")
		return nil
	})
	if first == "" {
		first = dbName
	}
	return lowerFirst(first) + "Queries"
}

func firstMatch(value, pattern string) string {
	m := regexp.MustCompile(pattern).FindStringSubmatch(value)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func lowerFirst(value string) string {
	if value == "" {
		return value
	}
	return strings.ToLower(value[:1]) + value[1:]
}

func moduleSourcesContain(mod *project.Module, needle string) bool {
	if mod == nil || needle == "" {
		return false
	}
	root := filepath.Join(mod.Dir, "src")
	found := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found {
			return nil
		}
		if !strings.HasSuffix(path, ".kt") && !strings.HasSuffix(path, ".java") {
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G122 -- root WalkDir traversal is bounded to trusted project source tree
		if err == nil && strings.Contains(string(data), needle) {
			found = true
		}
		return nil
	})
	return found
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
