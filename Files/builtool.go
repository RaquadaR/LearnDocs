package main

import (
        "encoding/xml"
        "fmt"
        "io/ioutil"
        "net/http"
        "os"
        "os/exec"
        "strings"
)

type PomProject struct {
        XMLName      xml.Name     `xml:"project"`
        GroupID      string       `xml:"groupId"`
        ArtifactID   string       `xml:"artifactId"`
        Version      string       `xml:"version"`
        Dependencies PomDependencies `xml:"dependencies"`
        Repositories PomRepositories `xml:"repositories"`
}

type PomRepositories struct {
        Repositories []PomRepository `xml:"repository"`
}

type PomRepository struct {
        ID  string `xml:"id"`
        URL string `xml:"url"`
}

type PomDependencies struct {
        Dependencies []PomDependency `xml:"dependency"`
}

type PomDependency struct {
        GroupID     string            `xml:"groupId"`
        ArtifactID  string            `xml:"artifactId"`
        Version     string            `xml:"version"`
        Scope       string            `xml:"scope"`
        Optional    string            `xml:"optional"`
        Exclusions  PomExclusions     `xml:"exclusions"`
}

type PomExclusions struct {
        Exclusions []PomExclusion `xml:"exclusion"`
}

type PomExclusion struct {
        GroupID    string `xml:"groupId"`
        ArtifactID string `xml:"artifactId"`
}

func loadPom(filename string) (PomProject, error) {
        data, err := ioutil.ReadFile(filename)
        if err != nil {
                return PomProject{}, err
        }

        var project PomProject
        err = xml.Unmarshal(data, &project)
        return project, err
}

func findRepositoryURL(project PomProject, groupID, artifactID, version string) string {
        // First try maven central.
        url := fmt.Sprintf("https://repo1.maven.org/maven2/%s/%s/%s/%s-%s.jar",
                strings.ReplaceAll(groupID, ".", "/"),
                artifactID,
                version,
                artifactID,
                version)
        if checkURL(url) {
                return url
        }

        // Then try other repositories defined in pom.xml.
        for _, repo := range project.Repositories.Repositories {
                url = fmt.Sprintf("%s/%s/%s/%s/%s-%s.jar",
                        strings.TrimSuffix(repo.URL, "/"),
                        strings.ReplaceAll(groupID, ".", "/"),
                        artifactID,
                        version,
                        artifactID,
                        version)
                if checkURL(url) {
                        return url
                }
        }

        return "" // Not found.
}

func checkURL(url string) bool {
        resp, err := http.Head(url)
        if err != nil {
                return false
        }
        defer resp.Body.Close()
        return resp.StatusCode == http.StatusOK
}

func buildDependencyGraph(project PomProject, graph map[string]*PomDependency, visited map[string]bool, exclusions map[string]bool, includeOptionals bool) {
        for _, dep := range project.Dependencies.Dependencies {
                key := fmt.Sprintf("%s:%s", dep.GroupID, dep.ArtifactID)
                if visited[key] {
                        continue
                }
                visited[key] = true;

                //Apply exclusions
                for _, exclude := range dep.Exclusions.Exclusions {
                        excludeKey := fmt.Sprintf("%s:%s", exclude.GroupID, exclude.ArtifactID)
                        exclusions[excludeKey] = true;
                }

                if dep.Optional == "true" && !includeOptionals {
                        continue; // Skip optional dependencies if not included.
                }

                graph[key] = &dep;

                depURL := findRepositoryURL(project, dep.GroupID, dep.ArtifactID, dep.Version)

                if depURL == ""{
                        fmt.Printf("Warning: dependency %s not found in any repository.\n", key)
                        continue;
                }

                pomURL := strings.TrimSuffix(depURL, ".jar") + ".pom"

                pomResp, pomErr := http.Get(pomURL)
                if pomErr != nil {
                        fmt.Printf("Warning: Failed to download pom for %s: %s\n", key, pomErr)
                        continue;
                }
                defer pomResp.Body.Close()

                pomData, pomReadErr := ioutil.ReadAll(pomResp.Body)
                if pomReadErr != nil {
                        fmt.Printf("Warning: Failed to read pom for %s: %s\n", key, pomReadErr)
                        continue;
                }
                var subProject PomProject
                xml.Unmarshal(pomData, &subProject)

                // Pass the current exclusion map and includeOptionals flag to the recursive call.
                buildDependencyGraph(subProject, graph, visited, exclusions, includeOptionals);
        }
}

func resolveDependencies(graph map[string]*PomDependency, exclusions map[string]bool, scope string) map[string]PomDependency {
        resolved := make(map[string]PomDependency)
        for _, dep := range graph {
                key := fmt.Sprintf("%s:%s", dep.GroupID, dep.ArtifactID)
                if exclusions[key] {
                        continue;
                }
                if scope != "" && dep.Scope != "" && dep.Scope != scope {
                        continue;
                }

                if _, ok := resolved[key]; !ok {
                        resolved[key] = *dep;
                } else {
                        // Very simplified version conflict resolution (last one wins)
                        resolved[key] = *dep;
                }
        }
        return resolved;
}

func downloadResolvedDependencies(project PomProject, resolved map[string]PomDependency) error {
    os.MkdirAll("build/lib", 0755)
    for _, dep := range resolved {
        url := findRepositoryURL(project, dep.GroupID, dep.ArtifactID, dep.Version)
        if url == ""{
            return fmt.Errorf("could not find repository for %s:%s:%s", dep.GroupID, dep.ArtifactID, dep.Version)
        }
        filename := fmt.Sprintf("build/lib/%s-%s.jar", dep.ArtifactID, dep.Version)
        fmt.Printf("Downloading: %s\n", url)
        resp, err := http.Get(url)
        if err != nil {
            return err
        }
        defer resp.Body.Close()

        data, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            return err
        }

        err = ioutil.WriteFile(filename, data, 0644)
        if err != nil {
            return err
        }
    }
    return nil;
}

// ... (runCommand and main functions)
