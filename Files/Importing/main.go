// main.go
package main

import (
    "flag"
    "fmt"
    "os"
    "sync"

    "github.com/yourusername/mavengo/dependency"
    "github.com/yourusername/mavengo/parser"
    "github.com/yourusername/mavengo/repository"
    "github.com/yourusername/mavengo/testrunner"
)

func main() {
    pomFile := flag.String("pom", "pom.xml", "Path to pom.xml file")
    flag.Parse()

    // Parse POM file
    pom, err := parser.ParsePOM(*pomFile)
    if err != nil {
        fmt.Printf("Error parsing POM: %v\n", err)
        os.Exit(1)
    }

    // Initialize repository manager
    repoManager := repository.NewManager(pom.Repositories)

    // Resolve dependencies concurrently
    var wg sync.WaitGroup
    depChan := make(chan *dependency.ResolvedDependency, 100)
    errChan := make(chan error, 100)

    resolver := dependency.NewResolver(repoManager)
    for _, dep := range pom.Dependencies {
        wg.Add(1)
        go func(d dependency.Dependency) {
            defer wg.Done()
            resolved, err := resolver.Resolve(d)
            if err != nil {
                errChan <- err
                return
            }
            depChan <- resolved
        }(dep)
    }

    // Handle results
    go func() {
        wg.Wait()
        close(depChan)
        close(errChan)
    }()

    dependencies := make(map[string]*dependency.ResolvedDependency)
    for {
        select {
        case dep := <-depChan:
            if dep != nil {
                dependencies[dep.Coordinates()] = dep
            }
        case err := <-errChan:
            if err != nil {
                fmt.Printf("Error resolving dependency: %v\n", err)
            }
        case <-time.After(30 * time.Second):
            fmt.Println("Timeout waiting for dependency resolution")
            return
        }
    }

    // Run tests if specified
    if pom.TestDependencies != nil {
        runner := testrunner.NewJUnitRunner(dependencies)
        err = runner.RunTests()
        if err != nil {
            fmt.Printf("Test execution failed: %v\n", err)
        }
    }
}