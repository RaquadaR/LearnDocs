package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Pom represents the structure of a pom.xml file
type Pom struct {
	XMLName        xml.Name        `xml:"project"`
	GroupID        string          `xml:"groupId"`
	ArtifactID     string          `xml:"artifactId"`
	Version        string          `xml:"version"`
	Packaging      string          `xml:"packaging"`
	Dependencies   Dependencies    `xml:"dependencies"`
	DependencyManagement DependencyManagement `xml:"dependencyManagement"`
	Repositories   Repositories    `xml:"repositories"`
	Build          Build           `xml:"build"`
	Properties     Properties      `xml:"properties"` // Add Properties
}

// Dependencies represents the <dependencies> tag
type Dependencies struct {
	Dependencies []Dependency `xml:"dependency"`
}

// Dependency represents a <dependency> tag
type Dependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
	Optional   string `xml:"optional"`
	Exclusions Exclusions `xml:"exclusions"`
}

// Exclusions represents the <exclusions> tag
type Exclusions struct {
	Exclusions []Exclusion `xml:"exclusion"`
}

// Exclusion represents an <exclusion> tag
type Exclusion struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
}

// DependencyManagement represents the <dependencyManagement> tag
type DependencyManagement struct {
	Dependencies []Dependency `xml:"dependency"`
}

// Repositories represents the <repositories> tag
type Repositories struct {
	Repositories []Repository `xml:"repository"`
}

// Repository represents a <repository> tag
type Repository struct {
	ID  string `xml:"id"`
	URL string `xml:"url"`
}

// Build represents the <build> tag
type Build struct {
	SourceDirectory  string `xml:"sourceDirectory"`
	TestSourceDirectory string `xml:"testSourceDirectory"`
	OutputDirectory  string `xml:"outputDirectory"`
	TestOutputDirectory string `xml:"testOutputDirectory"`
	Plugins          Plugins `xml:"plugins"`
}

// Plugins represents the <plugins> tag
type Plugins struct {
	Plugins []Plugin `xml:"plugin"`
}

// Plugin represents a <plugin> tag
type Plugin struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Executions Executions `xml:"executions"`
	Configuration   *xml.Node `xml:"configuration"` // Add Configuration
}

// Executions represents the <executions> tag
type Executions struct {
	Executions []Execution `xml:"execution"`
}

// Execution represents an <execution> tag
type Execution struct {
	ID    string `xml:"id"`
	Phase string `xml:"phase"`
	Goals Goals  `xml:"goals"`
}

// Goals represents the <goals> tag
type Goals struct {
	Goals []string `xml:"goal"`
}

// Properties represents the <properties> tag.  Crucial for variable substitution.
type Properties struct {
	Entries []PropertyEntry `xml:",any"`
}

// PropertyEntry is used to unmarshal individual properties.
type PropertyEntry struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

// ResolvedDependency represents a dependency with its resolved version and scope
type ResolvedDependency struct {
	GroupID    string
	ArtifactID string
	Version    string
	Scope      string
	PomURL     string
	JarURL     string
	Exclusions map[DependencyKey]bool
}

// DependencyKey represents a unique dependency identifier
type DependencyKey struct {
	GroupID    string
	ArtifactID string
}

// DependencyCache stores downloaded JAR files
var DependencyCache = make(map[string]string)

func main() {
	pomFile := "pom.xml"
	pom, err := readPom(pomFile)
	if err != nil {
		fmt.Println("Error reading pom.xml:", err)
		return
	}

	resolvedDependencies, err := resolveDependencies(pom)
	if err != nil {
		fmt.Println("Error resolving dependencies:", err)
		return
	}

	fmt.Println("Resolved Dependencies:")
	for _, dep := range resolvedDependencies {
		fmt.Printf("%s:%s:%s (Scope: %s)\n", dep.GroupID, dep.ArtifactID, dep.Version, dep.Scope)
	}

	// Example actions based on resolved dependencies
	err = downloadDependencies(resolvedDependencies, pom.Repositories)
	if err != nil {
		fmt.Println("Error downloading dependencies:", err)
		return
	}

	// Create directories if they don't exist
	if pom.Build.OutputDirectory == "" {
		pom.Build.OutputDirectory = "target/classes" // Default Maven value.
	}
	if pom.Build.TestOutputDirectory == "" {
		pom.Build.TestOutputDirectory = "target/test-classes"
	}
	os.MkdirAll(pom.Build.OutputDirectory, os.ModePerm)
	os.MkdirAll(pom.Build.TestOutputDirectory, os.ModePerm)

    // Compile
    if err := compileJavaCode(pom); err != nil {
        fmt.Println("Error compiling Java code:", err)
        return
    }

	// Run JUnit tests
	fmt.Println("\n--- Running JUnit Tests ---")
	if err := runJUnitTests(pom, resolvedDependencies); err != nil {
		fmt.Println("Error running JUnit tests:", err)
	}

    // Package
    if err := packageArtifact(pom); err != nil{
        fmt.Println("Error packaging the artifact", err)
    }
}

// readPom reads and parses the pom.xml file.
func readPom(pomFile string) (*Pom, error) {
	xmlFile, err := os.Open(pomFile)
	if err != nil {
		return nil, err
	}
	defer xmlFile.Close()

	byteValue, _ := ioutil.ReadAll(xmlFile)

	var pom Pom
	err = xml.Unmarshal(byteValue, &pom)
	if err != nil {
		return nil, err
	}
    // After unmarshalling, process properties
    pom.processProperties()
	return &pom, nil
}

// processProperties replaces variables in the POM with values from the <properties> section.
func (pom *Pom) processProperties() {
    if pom.Properties.Entries == nil {
        return
    }

    propertiesMap := make(map[string]string)
    for _, prop := range pom.Properties.Entries {
        propertiesMap[prop.XMLName.Local] = prop.Value
    }

    // Helper function to replace variables in a string
    replaceVars := func(s string) string {
        for key, value := range propertiesMap {
            s = strings.ReplaceAll(s, fmt.Sprintf("${%s}", key), value)
        }
        return s
    }

    // Apply variable replacement to relevant fields in the Pom struct.
    pom.GroupID = replaceVars(pom.GroupID)
    pom.ArtifactID = replaceVars(pom.ArtifactID)
    pom.Version = replaceVars(pom.Version)
    pom.Packaging = replaceVars(pom.Packaging)
    pom.Build.SourceDirectory = replaceVars(pom.Build.SourceDirectory)
    pom.Build.TestSourceDirectory = replaceVars(pom.Build.TestSourceDirectory)
    pom.Build.OutputDirectory = replaceVars(pom.Build.OutputDirectory)
    pom.Build.TestOutputDirectory = replaceVars(pom.Build.TestOutputDirectory)

    // Iterate through dependencies and replace variables
    for i := range pom.Dependencies.Dependencies {
        pom.Dependencies.Dependencies[i].GroupID = replaceVars(pom.Dependencies.Dependencies[i].GroupID)
        pom.Dependencies.Dependencies[i].ArtifactID = replaceVars(pom.Dependencies.Dependencies[i].ArtifactID)
        pom.Dependencies.Dependencies[i].Version = replaceVars(pom.Dependencies.Dependencies[i].Version)
        for j := range pom.Dependencies.Dependencies[i].Exclusions.Exclusions {
            pom.Dependencies.Dependencies[i].Exclusions.Exclusions[j].GroupID = replaceVars(pom.Dependencies.Dependencies[i].Exclusions.Exclusions[j].GroupID)
            pom.Dependencies.Dependencies[i].Exclusions.Exclusions[j].ArtifactID = replaceVars(pom.Dependencies.Dependencies[i].Exclusions.Exclusions[j].ArtifactID)
        }
    }

    for i := range pom.DependencyManagement.Dependencies {
        pom.DependencyManagement.Dependencies[i].GroupID = replaceVars(pom.DependencyManagement.Dependencies[i].GroupID)
        pom.DependencyManagement.Dependencies[i].ArtifactID = replaceVars(pom.DependencyManagement.Dependencies[i].ArtifactID)
        pom.DependencyManagement.Dependencies[i].Version = replaceVars(pom.DependencyManagement.Dependencies[i].Version)
        for j := range pom.DependencyManagement.Dependencies[i].Exclusions.Exclusions {
            pom.DependencyManagement.Dependencies[i].Exclusions.Exclusions[j].GroupID = replaceVars(pom.DependencyManagement.Dependencies[i].Exclusions.Exclusions[j].GroupID)
            pom.DependencyManagement.Dependencies[i].Exclusions.Exclusions[j].ArtifactID = replaceVars(pom.DependencyManagement.Dependencies[i].Exclusions.Exclusions[j].ArtifactID)
        }
    }
}

// resolveDependencies resolves the dependencies specified in the pom.xml file.
func resolveDependencies(pom *Pom) (map[DependencyKey]ResolvedDependency, error) {
	resolved := make(map[DependencyKey]ResolvedDependency)
	managedDependencies := make(map[DependencyKey]Dependency)

	// Process dependency management
	for _, dep := range pom.DependencyManagement.Dependencies {
		managedDependencies[DependencyKey{GroupID: dep.GroupID, ArtifactID: dep.ArtifactID}] = dep
	}

	var processDependency func(dep Dependency, scope string, exclusions map[DependencyKey]bool, optionalChain []DependencyKey) error
	processDependency = func(dep Dependency, scope string, exclusions map[DependencyKey]bool, optionalChain []DependencyKey) error {
		key := DependencyKey{GroupID: dep.GroupID, ArtifactID: dep.ArtifactID}

		// Check for circular optional dependencies
		for _, optionalDep := range optionalChain {
			if optionalDep == key {
				fmt.Printf("Warning: Circular optional dependency detected: %s:%s\n", dep.GroupID, dep.ArtifactID)
				return nil
			}
		}

		if _, excluded := exclusions[key]; excluded {
			return nil
		}

		resolvedDep := ResolvedDependency{
			GroupID:    dep.GroupID,
			ArtifactID: dep.ArtifactID,
			Version:    dep.Version,
			Scope:      scope,
			Exclusions: make(map[DependencyKey]bool),
		}

		// Apply version and scope from dependency management if present
		if managedDep, ok := managedDependencies[key]; ok {
			if managedDep.Version != "" {
				resolvedDep.Version = managedDep.Version
			}
			if managedDep.Scope != "" {
				resolvedDep.Scope = managedDep.Scope
			}
		}

		if dep.Version != "" {
			resolvedDep.Version = dep.Version // Override with direct dependency version
		}
		if dep.Scope != "" {
			resolvedDep.Scope = dep.Scope // Override with direct dependency scope
		}

		// Default scope to "compile" if not specified
		if resolvedDep.Scope == "" {
			resolvedDep.Scope = "compile"
		}

		if _, exists := resolved[key]; exists {
			// Handle version conflicts (keep the first resolved with a warning)
			if resolved[key].Version != resolvedDep.Version {
				fmt.Printf("Version conflict for %s:%s: %s vs %s (keeping %s)\n",
					resolvedDep.GroupID, resolvedDep.ArtifactID, resolved[key].Version, resolvedDep.Version, resolved[key].Version)
			}
			// Handle scope (closer scope wins, e.g., test > compile)
			if getScopePrecedence(resolvedDep.Scope) > getScopePrecedence(resolved[key].Scope) {
				resolved[key] = resolvedDep
			}
			// Merge exclusions (all exclusions from all paths are considered)
			for excludedKey := range dep.Exclusions.Exclusions {
				resolved[key].Exclusions[DependencyKey{GroupID: dep.Exclusions.Exclusions[excludedKey].GroupID, ArtifactID: dep.Exclusions.Exclusions[excludedKey].ArtifactID}] = true
			}
			return nil
		}

		resolved[key] = resolvedDep
		for _, exclusion := range dep.Exclusions.Exclusions {
			resolved[key].Exclusions[DependencyKey{GroupID: exclusion.GroupID, ArtifactID: exclusion.ArtifactID}] = true
		}

		// Fetch transitive dependencies
		transitiveDeps, err := fetchTransitiveDependencies(resolvedDep, pom.Repositories)
		if err == nil {
			newExclusions := make(map[DependencyKey]bool)
			for k, v := range exclusions {
				newExclusions[k] = v
			}
			for k := range resolved[key].Exclusions {
				newExclusions[k] = true
			}

			isOptional := strings.ToLower(dep.Optional) == "true"
			newOptionalChain := append(optionalChain, key)

			for _, transitiveDep := range transitiveDeps {
				if !newExclusions[DependencyKey{GroupID: transitiveDep.GroupID, ArtifactID: transitiveDep.ArtifactID}] {
					transitiveScope := transitiveDep.Scope
					if transitiveScope == "" {
						transitiveScope = resolvedDep.Scope // Inherit scope by default
					}
					// Optional dependency logic: only include if the parent is not optional or if it's a direct dependency
					if !isOptional || len(optionalChain) == 0 || transitiveScope == "test" {
						err := processDependency(transitiveDep, transitiveScope, newExclusions, newOptionalChain)
						if err != nil {
							fmt.Printf("Error processing transitive dependency %s:%s: %v\n", transitiveDep.GroupID, transitiveDep.ArtifactID, err)
						}
					} else {
						fmt.Printf("Skipping optional transitive dependency %s:%s via %s:%s\n", transitiveDep.GroupID, transitiveDep.ArtifactID, dep.GroupID, dep.ArtifactID)
					}
				}
			}
		} else {
			fmt.Printf("Warning: Could not fetch transitive dependencies for %s:%s:%s: %v\n", dep.GroupID, dep.ArtifactID, dep.Version, err)
		}
		return nil
	}

	// Process direct dependencies
	for _, dep := range pom.Dependencies.Dependencies {
		initialExclusions := make(map[DependencyKey]bool)
		for _, exclusion := range dep.Exclusions.Exclusions {
			initialExclusions[DependencyKey{GroupID: exclusion.GroupID, ArtifactID: exclusion.ArtifactID}] = true
		}
		err := processDependency(dep, dep.Scope, initialExclusions, []DependencyKey{})
		if err != nil {
			fmt.Printf("Error processing direct dependency %s:%s: %v\n", dep.GroupID, dep.ArtifactID, err)
		}
	}

	return resolved, nil
}

// fetchTransitiveDependencies fetches the transitive dependencies of a given dependency.
func fetchTransitiveDependencies(dep ResolvedDependency, repositories []Repository) ([]Dependency, error) {
	pomURL := constructArtifactURL(dep.GroupID, dep.ArtifactID, dep.Version, "pom", repositories)
	if pomURL == "" {
		return nil, fmt.Errorf("could not construct POM URL for %s:%s:%s", dep.GroupID, dep.ArtifactID, dep.Version)
	}
	dep.PomURL = pomURL

	resp, err := http.Get(pomURL)
	if err != nil {
		return nil, fmt.Errorf("error fetching POM for %s:%s:%s: %v", dep.GroupID, dep.ArtifactID, dep.Version, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not find POM for %s:%s:%s at %s (status: %d)", dep.GroupID, dep.ArtifactID, dep.Version, pomURL, resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading POM body for %s:%s:%s: %v", dep.GroupID, dep.ArtifactID, dep.Version, err)
	}

	var transitivePom Pom
	err = xml.Unmarshal(body, &transitivePom)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling transitive POM for %s:%s:%s: %v", dep.GroupID, dep.ArtifactID, dep.Version, err)
	}
    transitivePom.processProperties() //process properties.
	return transitivePom.Dependencies.Dependencies, nil
}

// constructArtifactURL constructs the URL for an artifact (POM or JAR).
func constructArtifactURL(groupID, artifactID, version, packaging string, repositories []Repository) string {
	artifactPath := strings.ReplaceAll(groupID, ".", "/") + "/" + artifactID + "/" + version + "/" + artifactID + "-" + version + "." + packaging
	for _, repo := range repositories {
		url := strings.TrimSuffix(repo.URL, "/") + "/" + artifactPath
		// Check if the artifact exists (basic HEAD request)
		resp, err := http.Head(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return url
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	// Default to Maven Central
	centralURL := "https://repo1.maven.org/maven2/" + artifactPath
	resp, err := http.Head(centralURL)
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return centralURL
	}
	if resp != nil {
		resp.Body.Close()
	}
	return ""
}

// downloadDependencies downloads the resolved dependencies.
func downloadDependencies(resolvedDependencies map[DependencyKey]ResolvedDependency, repositories []Repository) error {
	cacheDir := "dependencies_cache"
	os.MkdirAll(cacheDir, os.ModePerm)

	for _, dep := range resolvedDependencies {
		if dep.Scope == "test" {
			continue // Skip test-scoped dependencies for regular download
		}
		jarURL := constructArtifactURL(dep.GroupID, dep.ArtifactID, dep.Version, "jar", repositories)
		if jarURL == "" {
			fmt.Printf("Warning: Could not find JAR for %s:%s:%s\n", dep.GroupID, dep.ArtifactID, dep.Version)
			continue
		}
		dep.JarURL = jarURL
		cachePath := filepath.Join(cacheDir, fmt.Sprintf("%s-%s-%s.jar", dep.ArtifactID, dep.Version, dep.GroupID))

		if _, err := os.Stat(cachePath); os.IsNotExist(err) {
			fmt.Printf("Downloading %s:%s:%s from %s...\n", dep.GroupID, dep.ArtifactID, dep.Version, jarURL)
			resp, err := http.Get(jarURL)
			if err != nil {
				return fmt.Errorf("error downloading %s:%s:%s: %v", dep.GroupID, dep.ArtifactID, dep.Version, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("error downloading %s:%s:%s (status: %d)", dep.GroupID, dep.ArtifactID, dep.Version, resp.StatusCode)
			}

			out, err := os.Create(cachePath)
			if err != nil {
				return fmt.Errorf("error creating cache file for %s:%s:%s: %v", dep.GroupID, dep.ArtifactID, dep.Version, err)
			}
			defer out.Close()

			_, err = io.Copy(out, resp.Body)
			if err != nil {
				return fmt.Errorf("error writing to cache file for %s:%s:%s: %v", dep.GroupID, dep.ArtifactID, dep.Version, err)
			}
			DependencyCache[fmt.Sprintf("%s:%s:%s", dep.GroupID, dep.ArtifactID, dep.Version)] = cachePath
		} else {
			DependencyCache[fmt.Sprintf("%s:%s:%s", dep.GroupID, dep.ArtifactID, dep.Version)] = cachePath
		}
	}
	return nil
}

// getScopePrecedence returns the precedence of a dependency scope.
func getScopePrecedence(scope string) int {
	switch strings.ToLower(scope) {
	case "compile":
		return 1
	case "provided":
		return 2
	case "runtime":
		return 3
	case "test":
		return 4
	default:
		return 0 // Unknown scope
	}
}

// compileJavaCode compiles the Java source code.
func compileJavaCode(pom *Pom) error {
	// Default source directory
	sourceDir := "src/main/java"
	if pom.Build.SourceDirectory != "" {
		sourceDir = pom.Build.SourceDirectory
	}

	// Default output directory.
	outputDir := "target/classes"
	if pom.Build.OutputDirectory != ""{
		outputDir = pom.Build.OutputDirectory
	}

	// Find all Java files in the source directory
	javaFiles := []string{}
	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".java") {
			javaFiles = append(javaFiles, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error walking source directory: %v", err)
	}

	if len(javaFiles) == 0 {
		fmt.Println("No Java files found to compile.")
		return nil // No files to compile is not an error.
	}

	// Construct the classpath
	classpath := constructClasspath()

	// Construct the command.
	javacCmd := "javac"
	javacArgs := []string{"-d", outputDir, "-classpath", classpath}
	javacArgs = append(javacArgs, javaFiles...)

	fmt.Printf("Compiling Java code with command: %s %s\n", javacCmd, strings.Join(javacArgs, " "))
	cmd := exec.Command(javacCmd, javacArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("error running javac: %v", err)
	}
	return nil
}

// runJUnitTests runs the JUnit tests.
func runJUnitTests(pom *Pom, resolvedDependencies map[DependencyKey]ResolvedDependency) error {
    // Default test source directory.
    testSourceDir := "src/test/java"
    if pom.Build.TestSourceDirectory != "" {
        testSourceDir = pom.Build.TestSourceDirectory
    }
    //Default test output directory
    testOutputDir := "target/test-classes"
     if pom.Build.TestOutputDirectory != ""{
        testOutputDir = pom.Build.TestOutputDirectory
    }

    // Find all Java test files
    testJavaFiles := []string{}
    err := filepath.Walk(testSourceDir, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }
        if !info.IsDir() && strings.HasSuffix(info.Name(), "Test.java") { // Basic test file detection
            testJavaFiles = append(testJavaFiles, path)
        }
        return nil
    })
    if err != nil {
        return fmt.Errorf("error walking test source directory: %v", err)
    }
    if len(testJavaFiles) == 0{
        fmt.Println("No test files found")
        return nil
    }

    // Compile the test files.
    classpath := constructClasspath()
    javacCmd := "javac"
    javacArgs := []string{"-d", testOutputDir, "-classpath", classpath}
    javacArgs = append(javacArgs, testJavaFiles...)
    fmt.Printf("Compiling test Java code with command: %s %s\n", javacCmd, strings.Join(javacArgs, " "))

    cmd := exec.Command(javacCmd, javacArgs...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    err = cmd.Run()
    if err != nil {
        return fmt.Errorf("error compiling test code: %v", err)
    }
    // Construct the classpath for running the tests.  Include the test output directory.
    classpath = constructClasspath() + string(os.PathListSeparator) + testOutputDir

    // Use a simple command to run JUnit (assuming it's on the classpath).
	javaCmd := "java"
	javaArgs := []string{"-cp", classpath, "org.junit.runner.JUnitCore"} //Minimal, this requires junit in the classpath
    for _, testFile := range testJavaFiles{
        //Convert the file path to the class name.
        className := strings.ReplaceAll(strings.TrimSuffix(filepath.Base(testFile),".java"),".","/")+""
        javaArgs = append(javaArgs, className)
    }fmt.Printf("Running JUnit tests with command: %s %s\n", javaCmd, strings.Join(javaArgs, " "))
	cmd = exec.Command(javaCmd, javaArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("error running JUnit tests: %v", err)
	}
	return nil
}

// constructClasspath builds the classpath string.
func constructClasspath() string {
	classpath := ""
	for _, path := range DependencyCache {
		classpath += path + string(os.PathListSeparator)
	}
	// Add current directory (for compiled classes)
	classpath += "."
	return classpath
}

// packageArtifact packages the compiled code into a JAR file.
func packageArtifact(pom *Pom) error {
    // Default output directory
    outputDir := "target"
    if pom.Build.OutputDirectory != "" {
        outputDir = pom.Build.OutputDirectory
    }

    // Default jar name
    jarName := fmt.Sprintf("%s-%s.jar", pom.ArtifactID, pom.Version)
    jarPath := filepath.Join(outputDir, jarName)

    // Create the JAR file.
    jarFile, err := os.Create(jarPath)
    if err != nil {
        return fmt.Errorf("error creating JAR file: %v", err)
    }
    defer jarFile.Close()
    zipWriter := zip.NewWriter(jarFile)
    defer zipWriter.Close()

    // Walk through the output directory and add files to the JAR.
    err = filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }
        if info.IsDir() {
            return nil // Skip directories
        }

        // Get relative path within the output directory
        relPath, err := filepath.Rel(outputDir, path)
        if err != nil {
            return err
        }

        // Add file to the JAR
        zipEntry, err := zipWriter.Create(relPath)
        if err != nil {
            return fmt.Errorf("error creating JAR entry for %s: %v", relPath, err)
        }

        file, err := os.Open(path)
        if err != nil {
            return fmt.Errorf("error opening file %s: %v", path, err)
        }
        defer file.Close()

        _, err = io.Copy(zipEntry, file)
        if err != nil {
            return fmt.Errorf("error copying %s to JAR: %v", path, err)
        }
        return nil
    })
    if err != nil {
        return fmt.Errorf("error walking output directory: %v", err)
    }
    fmt.Printf("Successfully created JAR file: %s\n", jarPath)
    return nil
}
