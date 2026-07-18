package portable

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/10kkyvl/studioforge/internal/config"
	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/projects"
)

type Manifest struct {
	FormatVersion  int            `json:"formatVersion"`
	Product        string         `json:"product"`
	AppVersion     string         `json:"appVersion"`
	ExportedAt     time.Time      `json:"exportedAt"`
	Project        models.Project `json:"project"`
	Agents         []models.Agent `json:"agents"`
	Tasks          []models.Task  `json:"tasks"`
	IncludesSource bool           `json:"includesSource"`
}
type Preview struct {
	Manifest     Manifest `json:"manifest"`
	PathExists   bool     `json:"pathExists"`
	PathConflict bool     `json:"pathConflict"`
	NameConflict bool     `json:"nameConflict"`
	Warnings     []string `json:"warnings"`
}

func Export(ctx context.Context, store *database.Store, projectID, target string) error {
	project, err := store.Project(ctx, projectID)
	if err != nil {
		return err
	}
	agents, err := store.ListAgents(ctx, projectID)
	if err != nil {
		return err
	}
	tasks, err := store.ListTasks(ctx, projectID)
	if err != nil {
		return err
	}
	manifest := Manifest{FormatVersion: 1, Product: config.ProductName, AppVersion: config.Version, ExportedAt: time.Now().UTC(), Project: project, Agents: agents, Tasks: tasks, IncludesSource: false}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	file, err := os.Create(target)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(file)
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(target)
		}
	}()
	entry, err := zw.Create("manifest.json")
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(entry)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		return err
	}
	readme, err := zw.Create("README.txt")
	if err != nil {
		return err
	}
	_, err = io.WriteString(readme, "StudioForge portable project metadata export. Project source is intentionally not copied.\n")
	if err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func Read(path string) (Manifest, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return Manifest{}, err
	}
	defer zr.Close()
	for _, file := range zr.File {
		if file.Name != "manifest.json" {
			continue
		}
		if file.UncompressedSize64 > 4<<20 {
			return Manifest{}, errors.New("portable manifest is too large")
		}
		r, err := file.Open()
		if err != nil {
			return Manifest{}, err
		}
		var manifest Manifest
		err = json.NewDecoder(io.LimitReader(r, 4<<20)).Decode(&manifest)
		r.Close()
		if err != nil {
			return Manifest{}, err
		}
		if manifest.FormatVersion != 1 || manifest.Product != config.ProductName {
			return Manifest{}, errors.New("unsupported StudioForge export format")
		}
		return manifest, nil
	}
	return Manifest{}, errors.New("manifest.json is missing")
}
func Inspect(ctx context.Context, store *database.Store, path string) (Preview, error) {
	manifest, err := Read(path)
	if err != nil {
		return Preview{}, err
	}
	preview := Preview{Manifest: manifest}
	_, err = os.Stat(manifest.Project.Path)
	preview.PathExists = err == nil
	projectsList, err := store.ListProjects(ctx, true)
	if err != nil {
		return Preview{}, err
	}
	for _, p := range projectsList {
		if filepath.Clean(p.Path) == filepath.Clean(manifest.Project.Path) {
			preview.PathConflict = true
		}
		if p.Name == manifest.Project.Name {
			preview.NameConflict = true
		}
	}
	if !preview.PathExists {
		preview.Warnings = append(preview.Warnings, "The original project root is not present; choose an existing root before applying the import.")
	}
	if preview.PathConflict {
		preview.Warnings = append(preview.Warnings, "This project root is already registered.")
	}
	return preview, nil
}
func Apply(ctx context.Context, store *database.Store, path, newRoot, newName string) (models.Project, error) {
	preview, err := Inspect(ctx, store, path)
	if err != nil {
		return models.Project{}, err
	}
	if newRoot == "" {
		newRoot = preview.Manifest.Project.Path
	}
	root, err := projects.Canonical(newRoot)
	if err != nil {
		return models.Project{}, err
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return models.Project{}, fmt.Errorf("import root must be an existing directory")
	}
	if newName == "" {
		newName = preview.Manifest.Project.Name
	}
	if preview.PathConflict && filepath.Clean(root) == filepath.Clean(preview.Manifest.Project.Path) {
		return models.Project{}, errors.New("project root is already registered; choose another root")
	}
	return store.CreateProject(ctx, models.Project{Name: newName, Path: root, Fingerprint: projects.Fingerprint(root), Description: preview.Manifest.Project.Description})
}
