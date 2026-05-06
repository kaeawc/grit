package env

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kaeawc/grit/internal/project"
)

type Report struct {
	Items []Item
}

type Item struct {
	Name   string
	Detail string
	OK     bool
}

func Check(prj *project.Project) Report {
	items := []Item{
		checkCommand("java"),
		checkKotlinCompiler(prj),
		checkAndroidJar(),
		checkComposeCompilerPlugin(),
		checkSerializationCompilerPlugin(),
		checkMetroCompilerPlugin(),
	}
	return Report{Items: items}
}

func checkCommand(name string) Item {
	path, err := exec.LookPath(name)
	if err != nil {
		return Item{Name: name, Detail: "not found on PATH", OK: false}
	}
	return Item{Name: name, Detail: path, OK: true}
}

func checkKotlinCompiler(prj *project.Project) Item {
	if path, err := exec.LookPath("kotlinc"); err == nil {
		return Item{Name: "kotlinc", Detail: path, OK: true}
	}
	if path := LocateKotlinCompiler(prj); path != "" {
		return Item{Name: "kotlinc", Detail: "using Gradle cache: " + path, OK: true}
	}
	return Item{Name: "kotlinc", Detail: "not found on PATH or in Gradle cache", OK: false}
}

func checkAndroidJar() Item {
	path := filepath.Join(os.Getenv("HOME"), "Library", "Android", "sdk", "platforms", "android-36", "android.jar")
	_, err := os.Stat(path) // #nosec
	if err != nil {
		return Item{Name: "android.jar", Detail: path, OK: false}
	}
	return Item{Name: "android.jar", Detail: path, OK: true}
}

func checkComposeCompilerPlugin() Item {
	path := LocateComposeCompilerPlugin()
	if path == "" {
		return Item{Name: "compose-compiler-plugin", Detail: "not found alongside kotlinc or in Gradle cache", OK: false}
	}
	return Item{Name: "compose-compiler-plugin", Detail: path, OK: true}
}

func checkSerializationCompilerPlugin() Item {
	path := LocateSerializationCompilerPlugin()
	if path == "" {
		return Item{Name: "kotlin-serialization-compiler-plugin", Detail: "not found alongside kotlinc or in Gradle cache", OK: false}
	}
	return Item{Name: "kotlin-serialization-compiler-plugin", Detail: path, OK: true}
}

func checkMetroCompilerPlugin() Item {
	path := filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1", "dev.zacsweers.metro", "compiler", "0.12.0", "898e83c86c03300a76d55f83815ce13a1d1fc005", "compiler-0.12.0.jar")
	_, err := os.Stat(path) // #nosec
	if err != nil {
		return Item{Name: "metro-compiler-plugin", Detail: path, OK: false}
	}
	return Item{Name: "metro-compiler-plugin", Detail: path, OK: true}
}
