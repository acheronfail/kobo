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
)

type FirmwareVersion struct {
	Date     string `json: "date"`
	Download string `json: "download"`
	Version  string `json: "version"`
}

type Firmware struct {
	Hardware string            `json: "hardware"`
	Id       string            `json: "id"`
	Model    string            `json: "model"`
	Versions []FirmwareVersion `json: "versions"`
}

const (
	kobopatchDirectory            = "kobopatch"
	kobopatchPatchesBinDirectory  = "kobopatch-patches/src/template/bin"
	kobopatchPatchesSrcDirectory  = "kobopatch-patches/src/template/src"
	kobopatchPatchesSrcConfigFile = "kobopatch-patches/src/template/kobopatch.yaml"
)

var (
	version = flag.String("version", "4.19.14123", "version of the patch to create")
	uuid    = flag.String("uuid", "00000000-0000-0000-0000-000000000370", "uuid of the kobo (see firmwares.json)")
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] [versions...]\n\nOptions:\n", os.Args[0])
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

func replaceLineInFile(filepath, prefix, replace string) error {
	input, err := ioutil.ReadFile(filepath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(input), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[i] = replace
		}
	}

	output := strings.Join(lines, "\n")
	err = ioutil.WriteFile(filepath, []byte(output), 0644)
	if err != nil {
		return err
	}

	return nil
}

func mustBuildKobopatch(pkgPath, outfile string, extraArgs []string) {
	pkgPath, err := filepath.Rel(kobopatchDirectory, filepath.Join(kobopatchDirectory, pkgPath))
	if err != nil {
		log.Fatalln(err)
	}

	outfile, err = filepath.Rel(kobopatchDirectory, filepath.Join(kobopatchPatchesBinDirectory, outfile))
	if err != nil {
		log.Fatalln(err)
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
		log.Fatalln(err)
	}
}

func prepareKobopatch(v FirmwareVersion) error {
	// Build kobopatch and place in kobopatchPatchesBinDirectory
	// TODO: this can be cleaned up.
	switch fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH) {
	case "linux/amd64":
		mustBuildKobopatch("kobopatch", "kobopatch-linux-64bit", nil)
		mustBuildKobopatch("tools/cssextract", "kobopatch-apply-linux-64bit", nil)
		mustBuildKobopatch("tools/kobopatch-apply", "cssextract-linux-64bit", nil)
	case "linux/386":
		mustBuildKobopatch("kobopatch", "kobopatch-linux-32bit", nil)
		mustBuildKobopatch("tools/cssextract", "kobopatch-apply-linux-32bit", nil)
		mustBuildKobopatch("tools/kobopatch-apply", "cssextract-linux-32bit", nil)
	case "darwin/amd64":
		mustBuildKobopatch("kobopatch", "kobopatch-darwin-64bit", nil)
		mustBuildKobopatch("tools/cssextract", "cssextract-darwin-64bit", nil)
		mustBuildKobopatch("tools/kobopatch-apply", "kobopatch-apply-darwin-64bit", nil)
	case "windows/386":
		extraArgs := []string{"-ldflags \"-extldflags -static\""}
		mustBuildKobopatch("kobopatch", "koboptch-windows.exe", extraArgs)
		mustBuildKobopatch("tools/cssextract", "cssextract-windows.exe", extraArgs)
		mustBuildKobopatch("tools/kobopatch-apply", "koboptch-apply-windows.exe", extraArgs)
	}

	// Download the firmware first.
	_, err := downloadFirmware(v.Download)
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

	// Update kobopatch.yaml file with version.
	err = replaceLineInFile(kobopatchPatchesSrcConfigFile, "version: ", fmt.Sprintf("version: %s", *version))
	if err != nil {
		return err
	}

	err = replaceLineInFile(kobopatchPatchesSrcConfigFile, "in: ", fmt.Sprintf("in: src/kobo-update-%s.zip", *version))
	if err != nil {
		return err
	}

	// TODO: update kobopatch.yaml with overrides.

	fmt.Println("")
	fmt.Println("Patch files are ready!")
	fmt.Println("Update kobopatch-patches/src/template/kobopatch.yaml with your desired overrides.")
	fmt.Println("When ready, run the following to generate a patched firmware blob:")
	fmt.Println("")
	fmt.Println("  cd kobopatch-patches/src/template && ./kobopatch.sh && cd -")

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
