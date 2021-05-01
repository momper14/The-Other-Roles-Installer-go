package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/artdarek/go-unzip"
	"github.com/sqweek/dialog"
	"golang.org/x/sys/windows/registry"
)

type config struct {
	Autostart *bool
	GamePath  *string
}

var (
	client        = &http.Client{}
	toSkip        = []string{"Among Us_Data", "BepInEx", "Among Us.exe", "baselib.dll", "GameAssembly.dll", "UnityCrashHandler32.exe", "UnityPlayer.dll", "version.txt"}
	toSkipBepInEx = []string{"config"}

	autostart    = flag.Bool("autostart", false, "starts the game automatic on success")
	gamePathFlag = flag.String("gamePath", "", "path to the game")

	conf = &config{}

	appdata        = os.Getenv("APPDATA")
	configFilePath = appdata + `\Mo\the other roles installer\config.json`
)

func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func getGamePathManual() (string, error) {
	directory, err := dialog.Directory().Title("Couldn't autodetect the Among Us folder. \nPlease enter it manually.").Browse()

	for err == nil && !validateGamePath(directory) {
		directory, err = dialog.Directory().Title("The given folder doesn't contain a working Among Us installation. \nPlease try again.").Browse()
	}

	return directory, err
}

func getLatestVersion() (string, error) {
	req, _ := http.NewRequest("GET", "https://github.com/Eisbison/TheOtherRoles/releases/latest", nil)
	req.Header.Add("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Requesting last version failed with code %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	m := make(map[string]interface{})
	err = json.Unmarshal(body, &m)

	return m["tag_name"].(string), err
}

func exists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}

func validateGamePath(path string) bool {
	return exists(path + `\Among Us.exe`)
}

func getGamePathAuto() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, registry.QUERY_VALUE)
	if err != nil {
		k, err = registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Valve\Steam`, registry.QUERY_VALUE)
	}

	if err != nil {
		return "", err
	}

	defer k.Close()

	steamPath, _, err := k.GetStringValue("InstallPath")
	if err != nil {
		return "", err
	}

	gamePath := steamPath + `\steamapps\common\Among Us`

	if !validateGamePath(gamePath) {
		return gamePath, fmt.Errorf("Game not at the same location as steam")
	}

	return gamePath, err
}

func remove(s []fs.FileInfo, file string) []fs.FileInfo {
	for i, v := range s {
		if v.Name() == file {
			return append(s[:i], s[i+1:]...)
		}
	}

	return s
}

func cleanFolder(path string, skip []string) {
	content, _ := ioutil.ReadDir(path)

	for _, f := range skip {
		content = remove(content, f)
	}

	for _, f := range content {
		os.RemoveAll(path + `\` + f.Name())
	}
}

func cleanup(gamepath string) {
	cleanFolder(gamepath, toSkip)

	bepLnEx := gamepath + `\` + "BepInEx"
	if exists(bepLnEx) {
		cleanFolder(bepLnEx, toSkipBepInEx)
	}

}

func startGame() {
	exec.Command("rundll32", "url.dll,FileProtocolHandler", "steam://rungameid/945360").Start()
}

func downloadFile(url, filepath string) error {

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return err
}

func downloadAndExtractRelease(gamePath, version string) error {
	zipfile := gamePath + `\TheOtherRoles_` + version + ".zip"
	url := "https://github.com/Eisbison/TheOtherRoles/releases/download/" + version + "/TheOtherRoles.zip"

	if err := downloadFile(url, zipfile); err != nil {
		return err
	}
	if err := unzip.New(zipfile, gamePath).Extract(); err != nil {
		return err
	}

	if err := os.Remove(zipfile); err != nil {
		return err
	}

	if err := ioutil.WriteFile(gamePath+`\`+"version.txt", []byte(version), 0600); err != nil {
		return err
	}

	if *autostart {
		startGame()
	} else if dialog.Message("The Other Roles " + version + " is successfully installed. \nStart the game now?").Title("Successfully installed").YesNo() {
		startGame()
	}
	return nil
}

func errorExit(err error) {
	log.Println(err)
	dialog.Message(err.Error()).Title("Error").Error()
	os.Exit(1)
}

func getGamePath() string {

	if validateGamePath(*gamePathFlag) {
		return *gamePathFlag
	}

	if conf.GamePath != nil && validateGamePath(*conf.GamePath) {
		return *conf.GamePath
	}

	gamePath, err := getGamePathAuto()
	if err != nil {
		log.Println(err)
		gamePath, err = getGamePathManual()
	}
	if err != nil {
		if err == dialog.Cancelled {
			os.Exit(0)
		}
		errorExit(err)
	}

	return gamePath
}

func readConfig() {
	content, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		if _, ok := err.(*os.PathError); ok {
			return
		}
		errorExit(err)
	}

	if err := json.Unmarshal(content, conf); err != nil {
		errorExit(err)
	}
}

func writeConfig() {

	if isFlagPassed("autostart") {
		conf.Autostart = autostart
	}

	content, err := json.Marshal(&conf)
	if err != nil {
		errorExit(err)
	}

	if err := os.MkdirAll(filepath.Dir(configFilePath), 0700); err != nil {
		errorExit(err)
	}

	if err := ioutil.WriteFile(configFilePath, content, 0600); err != nil {
		errorExit(err)
	}
}

func main() {
	flag.Parse()

	readConfig()

	gamePath := getGamePath()
	conf.GamePath = &gamePath

	if !isFlagPassed("autostart") && conf.Autostart != nil {
		autostart = conf.Autostart
	}

	latestVersion, err := getLatestVersion()
	if err != nil {
		errorExit(err)
	}

	versionfilePath := gamePath + `\` + "version.txt"

	if !exists(versionfilePath) {
		cleanup(gamePath)

		if err := downloadAndExtractRelease(gamePath, latestVersion); err != nil {
			errorExit(err)
		}
		writeConfig()
		os.Exit(0)
	}

	installedVersion, err := ioutil.ReadFile(versionfilePath)
	if err != nil {
		errorExit(err)
	}

	if string(installedVersion) == latestVersion {
		if *autostart {
			startGame()
		} else if dialog.Message("The latest Version (" + latestVersion + ") is already installed. \nStart the game now?").Title("Already up to date").YesNo() {
			startGame()
		}
		writeConfig()
		os.Exit(0)
	}

	if err := downloadAndExtractRelease(gamePath, latestVersion); err != nil {
		errorExit(err)
	}
	writeConfig()

}
