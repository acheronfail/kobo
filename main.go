package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

type FirmwareVersion struct {
	Date     string `json:"date"`
	Download string `json:"download"`
	Version  string `json:"version"`
}

type Firmware struct {
	Hardware string            `json:"hardware"`
	Id       string            `json:"id"`
	Model    string            `json:"model"`
	Versions []FirmwareVersion `json:"versions"`
}

type KobopatchYaml struct {
	Version      string                     `yaml:"version"`
	In           string                     `yaml:"in"`
	Out          string                     `yaml:"out"`
	Log          string                     `yaml:"log"`
	PatchFormat  string                     `yaml:"patchFormat"`
	Patches      map[string]string          `yaml:"patches"`
	Overrides    map[string]map[string]bool `yaml:"overrides"`
	Lrelease     string                     `yaml:"lrelease,omitempty"`
	Translations map[string]string          `yaml:"translations,omitempty"`
	Files        map[string]interface{}     `yaml:"files,omitempty"`
}

const (
	kobopatchDirectory                = "kobopatch"
	kobopatchPatchesTemplateDirectory = "kobopatch-patches/src/template"
	kobopatchPatchesBinDirectory      = "kobopatch-patches/src/template/bin"
	kobopatchPatchesSrcDirectory      = "kobopatch-patches/src/template/src"
	kobopatchPatchesSrcConfigFile     = "kobopatch-patches/src/template/kobopatch.yaml"
	overridesFile                     = "overrides.yaml"
)

var (
	version = flag.String("version", "4.19.14123", "version of the patch to create")
	uuid    = flag.String("uuid", "00000000-0000-0000-0000-000000000370", "uuid of the kobo (see firmwares.json)")
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options]\n\nOptions:\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()
}

func downloadFirmware(url string) (string, error) {
	outfile := filepath.Join(kobopatchPatchesSrcDirectory, fmt.Sprintf("kobo-update-%s.zip", *version))
	if _, err := os.Stat(outfile); os.IsNotExist(err) {
		fmt.Println("Downloading: " + url)
		resp, err := http.Get(url)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()

		out, err := os.Create(outfile)
		if err != nil {
			return "", err
		}
		defer out.Close()

		_, err = io.Copy(out, resp.Body)
		if err != nil {
			return "", err
		}

		fmt.Println("Downloaded: " + outfile)
	} else {
		fmt.Println("Already exists: " + outfile)
	}

	return outfile, nil
}

func appendFileToFile(a, b string) error {
	extra, err := os.Open(a)
	if err != nil {
		return err
	}
	defer extra.Close()

	file, err := os.OpenFile(b, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, extra)
	if err != nil {
		return err
	}

	_, err = file.WriteString("\n")
	return err
}

func updateKobopatchYaml() error {
	kobopatchYamlFile, err := ioutil.ReadFile(kobopatchPatchesSrcConfigFile)
	if err != nil {
		return err
	}

	var kobopatchYaml KobopatchYaml
	err = yaml.Unmarshal(kobopatchYamlFile, &kobopatchYaml)
	if err != nil {
		return err
	}

	overridesYamlFile, err := ioutil.ReadFile(overridesFile)
	if err != nil {
		return err
	}

	var overridesYaml KobopatchYaml
	err = yaml.Unmarshal(overridesYamlFile, &overridesYaml)
	if err != nil {
		return err
	}

	kobopatchYaml.Version = *version
	kobopatchYaml.In = fmt.Sprintf("src/kobo-update-%s.zip", *version)
	kobopatchYaml.Overrides = overridesYaml.Overrides

	kobopatchYamlUpdated, err := yaml.Marshal(kobopatchYaml)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(kobopatchPatchesSrcConfigFile, kobopatchYamlUpdated, 0644)
	if err != nil {
		return err
	}

	return nil
}

func buildKobopatch() error {
	buildPackage := func(pkgPath, outfile string, extraArgs []string) error {
		pkgPath, err := filepath.Rel(kobopatchDirectory, filepath.Join(kobopatchDirectory, pkgPath))
		if err != nil {
			return err
		}

		outfile, err = filepath.Rel(kobopatchDirectory, filepath.Join(kobopatchPatchesBinDirectory, outfile))
		if err != nil {
			return err
		}

		args := []string{fmt.Sprintf("-o=%s", outfile), fmt.Sprintf("./%s", pkgPath)}
		if extraArgs != nil {
			args = append(args, extraArgs...)
		}
		args = append([]string{"build"}, args...)
		fmt.Println(fmt.Sprintf("go %s", strings.Join(args, " ")))

		cmd := exec.Command("go", args...)
		cmd.Dir = kobopatchDirectory

		if err := cmd.Run(); err != nil {
			return err
		}

		return nil
	}

	var extraArgs []string
	buildMap := make(map[string]string)

	switch fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH) {
	case "linux/amd64":
		buildMap["kobopatch"] = "kobopatch-linux-64bit"
		buildMap["tools/cssextract"] = "kobopatch-apply-linux-64bit"
		buildMap["tools/kobopatch-apply"] = "cssextract-linux-64bit"
	case "linux/386":
		buildMap["kobopatch"] = "kobopatch-linux-32bit"
		buildMap["tools/cssextract"] = "kobopatch-apply-linux-32bit"
		buildMap["tools/kobopatch-apply"] = "cssextract-linux-32bit"
	case "darwin/amd64":
		buildMap["kobopatch"] = "kobopatch-darwin-64bit"
		buildMap["tools/cssextract"] = "cssextract-darwin-64bit"
		buildMap["tools/kobopatch-apply"] = "kobopatch-apply-darwin-64bit"
	case "windows/386":
		extraArgs = []string{"-ldflags \"-extldflags -static\""}
		buildMap["kobopatch"] = "koboptch-windows.exe"
		buildMap["tools/cssextract"] = "cssextract-windows.exe"
		buildMap["tools/kobopatch-apply"] = "koboptch-apply-windows.exe"
	}

	for pkg, out := range buildMap {
		err := buildPackage(pkg, out, extraArgs)
		if err != nil {
			return err
		}
	}

	return nil
}

func prepareKobopatch(v FirmwareVersion) error {
	// Build kobopatch and place in kobopatchPatchesBinDirectory
	err := buildKobopatch()
	if err != nil {
		return err
	}

	// Download the firmware first.
	_, err = downloadFirmware(v.Download)
	if err != nil {
		return err
	}

	// Remove any pre-existing yaml files in the kobopatch-patches src directory.
	err = filepath.Walk(kobopatchPatchesSrcDirectory, func(path string, f os.FileInfo, err error) error {
		if !f.IsDir() {
			if filepath.Ext(path) == ".yaml" {
				err := os.Remove(path)
				return err
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Concat all patches together.
	patchfilesDirectory := fmt.Sprintf("kobopatch-patches/src/versions/%s", *version)
	err = filepath.Walk(patchfilesDirectory, func(path string, f os.FileInfo, err error) error {
		if !f.IsDir() {
			yamlFile := filepath.Join(kobopatchPatchesSrcDirectory, filepath.Base(filepath.Dir(path)))
			err := appendFileToFile(path, yamlFile)
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Update kobopatch.yaml with version and overrides.
	err = updateKobopatchYaml()
	if err != nil {
		return err
	}

	// Run kobopatch.
	cmd := exec.Command("./kobopatch.sh")
	cmd.Dir = kobopatchPatchesTemplateDirectory
	cmd.Stdout = os.Stdout

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func main() {
	firmwareFile, err := ioutil.ReadFile("firmwares.json")
	if err != nil {
		log.Fatalln(err)
	}

	var firmwares []Firmware
	err = json.Unmarshal(firmwareFile, &firmwares)
	if err != nil {
		log.Fatalln(err)
	}

	for _, fw := range firmwares {
		if fw.Id == *uuid {
			for _, v := range fw.Versions {
				if v.Version == *version {
					err := prepareKobopatch(v)
					if err != nil {
						log.Fatalln(err)
					}

					return
				}
			}
		}
	}

	log.Fatalln("firmware not found!")
}
