// testrunner/junit.go
package testrunner

import (
    "os/exec"
    "runtime"

    "github.com/yourusername/mavengo/dependency"
)

type JUnitRunner struct {
    deps map[string]*dependency.ResolvedDependency
}

func NewJUnitRunner(deps map[string]*dependency.ResolvedDependency) *JUnitRunner {
    return &JUnitRunner{deps: deps}
}

func (r *JUnitRunner) RunTests() error {
    // Configure JVM memory settings based on available resources
    mem := r.calculateMemory()
    
    var classpath []string
    for _, dep := range r.deps {
        classpath = append(classpath, dep.Path)
    }

    cmd := exec.Command("java",
        fmt.Sprintf("-Xmx%dm", mem),
        "-cp", strings.Join(classpath, string(os.PathListSeparator)),
        "org.junit.runner.JUnitCore",
    )

    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("test execution failed: %v\n%s", err, output)
    }
    fmt.Println(string(output))
    return nil
}

func (r *JUnitRunner) calculateMemory() int {
    var mem runtime.MemStats
    runtime.ReadMemStats(&mem)
    available := int(mem.Sys / 1024 / 1024) // Convert to MB
    return available * 3 / 4                // Use 75% of available memory
}