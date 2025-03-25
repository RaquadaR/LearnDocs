// dependency/resolver.go
package dependency

import (
    "fmt"
    "sync"

    "github.com/yourusername/mavengo/repository"
)

type Dependency struct {
    GroupID    string `xml:"groupId"`
    ArtifactID string `xml:"artifactId"`
    Version    string `xml:"version"`
    Scope      string `xml:"scope,omitempty"`
    Optional   bool   `xml:"optional,omitempty"`
    Exclusions []Exclusion `xml:"exclusions>exclusion"`
}

type Exclusion struct {
    GroupID    string `xml:"groupId"`
    ArtifactID string `xml:"artifactId"`
}

type ResolvedDependency struct {
    Dependency
    TransitiveDeps []*ResolvedDependency
    Path           string
}

type Resolver struct {
    repoManager *repository.Manager
    cache       map[string]*ResolvedDependency
    mutex       sync.Mutex
}

func NewResolver(repoManager *repository.Manager) *Resolver {
    return &Resolver{
        repoManager: repoManager,
        cache:       make(map[string]*ResolvedDependency),
    }
}

func (r *Resolver) Resolve(dep Dependency) (*ResolvedDependency, error) {
    coords := fmt.Sprintf("%s:%s:%s", dep.GroupID, dep.ArtifactID, dep.Version)
    
    r.mutex.Lock()
    if cached, ok := r.cache[coords]; ok {
        r.mutex.Unlock()
        return cached, nil
    }
    r.mutex.Unlock()

    // Download artifact
    path, err := r.repoManager.Download(dep)
    if err != nil {
        return nil, err
    }

    resolved := &ResolvedDependency{
        Dependency: dep,
        Path:       path,
    }

    // Resolve transitive dependencies
    pomPath := r.repoManager.GetPOMPath(dep)
    if pomPath != "" {
        pomData, err := os.ReadFile(pomPath)
        if err == nil {
            var pom struct {
                Dependencies []Dependency `xml:"dependencies>dependency"`
            }
            if xml.Unmarshal(pomData, &pom) == nil {
                for _, transDep := range pom.Dependencies {
                    if !r.isExcluded(transDep, dep.Exclusions) {
                        resolvedDep, err := r.Resolve(transDep)
                        if err == nil {
                            resolved.TransitiveDeps = append(resolved.TransitiveDeps, resolvedDep)
                        }
                    }
                }
            }
        }
    }

    r.mutex.Lock()
    r.cache[coords] = resolved
    r.mutex.Unlock()

    return resolved, nil
}

func (r *Resolver) isExcluded(dep Dependency, exclusions []Exclusion) bool {
    for _, ex := range exclusions {
        if ex.GroupID == dep.GroupID && ex.ArtifactID == dep.ArtifactID {
            return true
        }
    }
    return false
}

func (rd *ResolvedDependency) Coordinates() string {
    return fmt.Sprintf("%s:%s:%s", rd.GroupID, rd.ArtifactID, rd.Version)
}