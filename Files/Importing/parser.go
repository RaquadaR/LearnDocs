// parser/pom.go
package parser

import (
    "encoding/xml"
    "os"
    "github.com/yourusername/mavengo/dependency"
)

type POM struct {
    XMLName         xml.Name            `xml:"project"`
    Dependencies    []dependency.Dependency `xml:"dependencies>dependency"`
    Repositories    []Repository        `xml:"repositories>repository"`
    TestDependencies []dependency.Dependency `xml:"testDependencies>dependency"`
}

type Repository struct {
    ID  string `xml:"id"`
    URL string `xml:"url"`
}

func ParsePOM(filename string) (*POM, error) {
    data, err := os.ReadFile(filename)
    if err != nil {
        return nil, err
    }

    var pom POM
    err = xml.Unmarshal(data, &pom)
    if err != nil {
        return nil, err
    }

    return &pom, nil
}