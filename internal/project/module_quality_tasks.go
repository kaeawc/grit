package project

import (
	"sort"
	"strings"
)

func (m Module) qualityTasks() []Task {
	var out []Task
	if m.hasPluginContaining("ktlint") {
		out = append(out, m.ktlintTasks()...)
	}
	if m.hasPluginContaining("detekt") {
		out = append(out, m.detektTasks()...)
	}
	if len(out) == 0 {
		return nil
	}
	return uniqueSortedTasks(out)
}

func (m Module) hasPluginContaining(fragment string) bool {
	fragment = strings.ToLower(strings.TrimSpace(fragment))
	if fragment == "" {
		return false
	}
	for _, plugin := range m.Plugins {
		if strings.Contains(strings.ToLower(strings.TrimSpace(plugin)), fragment) {
			return true
		}
	}
	return false
}

func (m Module) ktlintTasks() []Task {
	tasks := []Task{
		{Name: "ktlintCheck", Category: "verification", Description: "Run ktlint checks.", Supported: false},
		{Name: "ktlintFormat", Category: "formatting", Description: "Run ktlint formatter.", Supported: false},
		{Name: "ktlintGenerateBaseline", Category: "verification", Description: "Generate ktlint baseline.", Supported: false},
	}
	for _, sourceSet := range m.qualitySourceSetNames() {
		suffix := taskNameSuffix(sourceSet)
		tasks = append(tasks,
			Task{Name: "runKtlintCheckOver" + suffix + "SourceSet", Category: "verification", Description: "Run ktlint over " + sourceSet + " sources.", Supported: false},
			Task{Name: "runKtlintFormatOver" + suffix + "SourceSet", Category: "formatting", Description: "Format " + sourceSet + " sources with ktlint.", Supported: false},
		)
	}
	return tasks
}

func (m Module) detektTasks() []Task {
	tasks := []Task{
		{Name: "detekt", Category: "verification", Description: "Run detekt analysis.", Supported: false},
		{Name: "detektBaseline", Category: "verification", Description: "Generate detekt baseline.", Supported: false},
		{Name: "detektGenerateConfig", Category: "verification", Description: "Generate detekt configuration.", Supported: false},
		{Name: "detektMain", Category: "verification", Description: "Run detekt for main sources.", Supported: false},
		{Name: "detektBaselineMain", Category: "verification", Description: "Generate detekt baseline for main sources.", Supported: false},
		{Name: "detektMainSourceSet", Category: "verification", Description: "Run detekt for the main source set.", Supported: false},
		{Name: "detektBaselineMainSourceSet", Category: "verification", Description: "Generate detekt baseline for the main source set.", Supported: false},
	}
	for _, variant := range m.Variants() {
		name := strings.TrimSpace(variant.Name)
		if name == "" {
			continue
		}
		suffix := taskNameSuffix(name)
		unitSuffix := suffix + "UnitTest"
		if m.IsJVM() && strings.EqualFold(name, "main") {
			unitSuffix = "Test"
		}
		tasks = append(tasks,
			Task{Name: "detekt" + suffix, Category: "verification", Description: "Run detekt for " + name + ".", Supported: false},
			Task{Name: "detekt" + suffix + "SourceSet", Category: "verification", Description: "Run detekt for the " + name + " source set.", Supported: false},
			Task{Name: "detekt" + unitSuffix, Category: "verification", Description: "Run detekt for " + name + " unit tests.", Supported: false},
			Task{Name: "detekt" + unitSuffix + "SourceSet", Category: "verification", Description: "Run detekt for the " + name + " unit-test source set.", Supported: false},
			Task{Name: "detektBaseline" + suffix, Category: "verification", Description: "Generate detekt baseline for " + name + ".", Supported: false},
			Task{Name: "detektBaseline" + suffix + "SourceSet", Category: "verification", Description: "Generate detekt baseline for the " + name + " source set.", Supported: false},
			Task{Name: "detektBaseline" + unitSuffix, Category: "verification", Description: "Generate detekt baseline for " + name + " unit tests.", Supported: false},
			Task{Name: "detektBaseline" + unitSuffix + "SourceSet", Category: "verification", Description: "Generate detekt baseline for the " + name + " unit-test source set.", Supported: false},
		)
	}
	return tasks
}

func (m Module) qualitySourceSetNames() []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	add("main")
	if m.IsJVM() {
		add("test")
	}
	for _, variant := range m.Variants() {
		resolved := m.ResolveVariant(variant.Name)
		for _, sourceSet := range resolved.SourceSetOrder {
			add(sourceSet)
		}
		if name := strings.TrimSpace(variant.Name); name != "" && !m.IsJVM() {
			add("test" + taskNameSuffix(name))
		}
	}
	sort.Strings(out)
	return out
}

func uniqueSortedTasks(tasks []Task) []Task {
	if len(tasks) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		name := strings.TrimSpace(task.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		task.Name = name
		out = append(out, task)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}
