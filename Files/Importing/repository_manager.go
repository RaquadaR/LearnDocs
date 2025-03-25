// repository/manager.go
package repository

import (
    "fmt"
    "net/http"
    "os"
    "path/filepath"
    "sync"

    "github.com/yourusername/mavengo/dependency"
)

type Manager struct {
    repos     []Repository
    cacheDir  string
    downloads sync.Map
}

func NewManager(repos []Repository) *Manager {
    home, _ := os.UserHomeDir()
    return &Manager{
        repos:    append(repos, Repository{ID: "central", URL: "https://repo.maven.apache.org/maven2"}),
        cacheDir: filepath.Join(home, ".mavengo", "cache"),
    }
}

func (m *Manager) Download(dep dependency.Dependency) (string, error) {
    path := m.getArtifactPath(dep)
    if _, err := os.Stat(path); err == nil {
        return path, nil
    }

    // Check if already downloading
    if _, loaded := m.downloads.LoadOrStore(path, true); loaded {
        // Wait for existing download
        for i := 0; i < 30; i++ {
            if _, err := os.Stat(path); err == nil {
                return path, nil
            }
            time.Sleep(time.Second)
        }
    }

    for _, repo := range m.repos {
        url := m.getArtifactURL(repo.URL, dep)
        resp, err := http.Get(url)
        if err != nil || resp.StatusCode != 200 {
            continue
        }
        defer resp.Body.Close()

        os.MkdirAll(filepath.Dir(path), 0755)
        out, err := os.Create(path)
        if err != nil {
            return "", err
        }
        defer out.Close()

        _, err = io.Copy(out, resp.Body)
        if err != nil {
            return "", err
        }
        m.downloads.Delete(path)
        return path, nil
    }

    return "", fmt.Errorf("could not download %s:%s:%s", dep.GroupID, dep.ArtifactID, dep.Version)
}

func (m *Manager) getArtifactPath(dep dependency.Dependency) string {
    return filepath.Join(m.cacheDir,
        strings.ReplaceAll(dep.GroupID, ".", "/"),
        dep.ArtifactID,
        dep.Version,
        fmt.Sprintf("%s-%s.jar", dep.ArtifactID, dep.Version))
}

func (m *Manager) getArtifactURL(baseURL string, dep dependency.Dependency) string {
    return fmt.Sprintf("%s/%s/%s/%s/%s-%s.jar",
        baseURL,
        strings.ReplaceAll(dep.GroupID, ".", "/"),
        dep.ArtifactID,
        dep.Version,
        dep.ArtifactID,
        dep.Version)
}

func (m *Manager) GetPOMPath(dep dependency.Dependency) string {
    // Similar to getArtifactPath but with .pom extension
    // Implementation omitted for brevity
}