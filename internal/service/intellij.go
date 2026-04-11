package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/kaeawc/grit/internal/integration"
	"github.com/kaeawc/grit/internal/intellijsync"
	"github.com/kaeawc/grit/internal/intellijtask"
	"github.com/kaeawc/grit/internal/project"
)

func (s *Service) IntelliJSyncModel(ctx context.Context, prj *project.Project) (*intellijsync.Model, error) {
	model, err := s.LoadConfigurationModel(ctx, prj)
	if err != nil {
		return nil, err
	}
	return intellijsync.Builder{}.Build(model, prj)
}

func (s *Service) IntegrationView(ctx context.Context, prj *project.Project) (*integration.ModelView, error) {
	model, err := s.LoadConfigurationModel(ctx, prj)
	if err != nil {
		return nil, err
	}
	return integration.NewModelView(model), nil
}

func (s *Service) ResolveIntelliJTaskRequests(prj *project.Project, req intellijtask.Request) ([]BuildRequest, error) {
	if len(req.Settings.TaskNames) == 0 {
		return nil, fmt.Errorf("taskNames is required")
	}
	var out []BuildRequest
	for _, rawTask := range req.Settings.TaskNames {
		resolved, err := resolveIntelliJTaskRequest(prj, req.Settings, rawTask)
		if err != nil {
			return nil, err
		}
		out = append(out, resolved...)
	}
	return out, nil
}

func resolveIntelliJTaskRequest(prj *project.Project, settings intellijtask.Settings, rawTask string) ([]BuildRequest, error) {
	if settings.ModulePath == "" {
		settings.ModulePath = qualifiedTaskModulePath(rawTask)
	}
	if settings.ModulePath == "" {
		return nil, fmt.Errorf("module path is required for task %q", rawTask)
	}
	if settings.ModuleKind == intellijtask.ModuleKindUnknown {
		mod := prj.FindModule(settings.ModulePath)
		if mod == nil {
			return nil, fmt.Errorf("module %s not found", settings.ModulePath)
		}
		settings.ModuleKind = intellijtask.ModuleKind(mod.Type)
	}
	resolved, err := (intellijtask.Request{Settings: intellijtask.Settings{
		ExternalProjectPath: settings.ExternalProjectPath,
		ModulePath:          settings.ModulePath,
		ModuleKind:          settings.ModuleKind,
		TaskNames:           []string{rawTask},
		RequestedVariant:    settings.RequestedVariant,
		VariantExplicit:     settings.VariantExplicit,
		DeviceSerial:        settings.DeviceSerial,
		ScriptParameters:    append([]string(nil), settings.ScriptParameters...),
		VMOptions:           append([]string(nil), settings.VMOptions...),
	}}).Resolve()
	if err != nil {
		return nil, err
	}
	out := make([]BuildRequest, 0, len(resolved))
	for _, item := range resolved {
		out = append(out, BuildRequest{
			ModulePath:       item.ModulePath,
			TaskName:         item.TaskName,
			Command:          item.Command,
			RequestedVariant: item.RequestedVariant,
			VariantExplicit:  item.VariantExplicit,
			DeviceSerial:     item.DeviceSerial,
		})
	}
	return out, nil
}

func qualifiedTaskModulePath(task string) string {
	task = strings.TrimSpace(task)
	if task == "" {
		return ""
	}
	parts := strings.Split(task, ":")
	var filtered []string
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	if len(filtered) < 2 {
		return ""
	}
	return ":" + strings.Join(filtered[:len(filtered)-1], ":")
}
