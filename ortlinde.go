package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const (
	SESSION_PERSIST_FILE = "./session.ort"
	SDVX_LAUNCH_URL      = "https://p.eagate.573.jp/game/konasteapp/API/login/login.html?game_id=sdvx&refresh=true"
	SDVX_MAINTENANCE     = "メンテナンス"
)

func main() {
	launchDirect := true
	if len(os.Args) > 1 {
		if os.Args[1] == "launcher" {
			launchDirect = false
		}
	}

	fmt.Println(`

     [[ Ortlinde: Simple SDVX コナステ Launcher ]]


[INIT] Checking login...`)
	session, expiry := getPersistedLogin()
	if expiry != -1 {
		fmt.Println("[AUTH] Checking login validity...")
		expired := time.Now().Unix() > expiry
		if expired {
			fmt.Println("[AUTH] Session seems to have expired!")
			session = fetchSessionFromUser()
		} else {
			fmt.Println("[AUTH] Session seems to still be valid, will try and use it.")
		}
	} else {
		fmt.Println("[AUTH] No persisted session found!")
		session = fetchSessionFromUser()
	}

	fmt.Println("[AUTH.573] Fetching SDVX launch token...")
	sdvxToken, result := getSdvxLaunchToken(session)
	for result != SdvxLaunchOk {
		switch result {
		case SdvxLaunchMaintenance:
			fmt.Println("\n[AUTH.573] The game is currently undergoing MAINTENANCE! See you next play :)")
			os.Exit(0)
		case SdvxLaunchUnauthorised:
			fmt.Println("[AUTH.573] Unauthorised. Are you sure your session token was passed in properly?")
			session = fetchSessionFromUser()
			sdvxToken, result = getSdvxLaunchToken(session)
		default:
			panic("unreachable")
		}
	}

	fmt.Println("[SDVX] Finding game folder...")
	gameFolder := determineOrAskSdvxFolder()

	fmt.Println("[SDVX] Launching game...")
	launchSdvx(gameFolder, sdvxToken, launchDirect)

	fmt.Println("\n\n     << Thanks for using Ortlinde! See you next play :) >>\n")
}

func getPersistedLogin() (string, int64) {
	_, err := os.Stat(SESSION_PERSIST_FILE)
	if err != nil {
		if os.IsNotExist(err) {
			return "", -1
		}
		panic(err)
	}

	data, err := os.ReadFile(SESSION_PERSIST_FILE)
	if err != nil {
		panic(err)
	}

	splitData := strings.SplitN(string(data), "\t", 2)
	expiry, err := strconv.ParseInt(splitData[1], 10, 64)
	if err != nil {
		panic(err)
	}

	return splitData[0], expiry
}

func fetchSessionFromUser() string {
	fmt.Print(`

An authentication token is required for Ortlinde to launch the game for you.

Please provide one by visiting: https://p.eagate.573.jp/game/eacsdvx/vi/index.html, ensuring
you are logged into your KONAMI ID (e-amusement pass should show up at the top), then opening 
'Dev Tools' by hitting Ctrl+Shift+I. From there, go to Application -> Cookies -> p.eagate.573.jp,
and copy the 'M573SSID' session token 'Value' here!

Enter M573SSID value: `)

	var session string
	_, err := fmt.Scanln(&session)
	if err != nil {
		panic(err)
	}
	fmt.Println("")

	return session
}

type SdvxLaunchStatus int

const (
	SdvxLaunchOk = iota
	SdvxLaunchMaintenance
	SdvxLaunchUnauthorised
	// Other errors we will panic
)

func getSdvxLaunchToken(session string) (string, SdvxLaunchStatus) {
	launchUrl, err := url.Parse(SDVX_LAUNCH_URL)
	if err != nil {
		panic(err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		panic(err)
	}
	cookie := http.Cookie{
		Name:  "M573SSID",
		Value: session,
	}
	jar.SetCookies(launchUrl, []*http.Cookie{&cookie})

	// Request with cookies + header
	req, err := http.NewRequest("GET", SDVX_LAUNCH_URL, nil)
	if err != nil {
		panic(err)
	}
	req.Header = http.Header{
		"User-Agent": {"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Safari/537.36"},
	}

	// Make request, don't use redirect (so can detect unauth)
	client := http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}

	if resp.StatusCode == http.StatusFound {
		return "", SdvxLaunchUnauthorised
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("ERROR: Failed to fetch SDVX launch token! (%d)\n", resp.StatusCode)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Auth OK, let's store it for future (it refreshes daily itself if you play)
	for _, cookie := range jar.Cookies(launchUrl) {
		// TODO: go doesn't seem to consider the cookie expiration properly
		//       cookies last for 1 week, but to be safe will just persist as 6.
		//       they should update everyday anyway.
		if cookie.Name == "M573SSID" {
			fmt.Println("[AUTH.573] Persisting session...")
			persist573Session(cookie.Value, time.Now().AddDate(0, 0, 6).Unix())
			break
		}
	}

	// Read contents and check for maint
	bodyData, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	body := string(bodyData)

	if strings.Contains(body, SDVX_MAINTENANCE) {
		return "", SdvxLaunchMaintenance
	}

	// Find token
	re := regexp.MustCompile(`'konaste\.sdvx:\/\/login\?tk=([a-z0-9\-]+)'`)
	matches := re.FindStringSubmatch(body)
	if matches == nil {
		fmt.Println("ERROR: Failed to find launch token! Either something is wrong with Konami's side, or Ortlinde!")
		os.Exit(1)
	}
	token := matches[1]

	return token, SdvxLaunchOk
}

func persist573Session(session string, expiry int64) {
	data := fmt.Sprintf("%s\t%d", session, expiry)
	err := os.WriteFile(SESSION_PERSIST_FILE, []byte(data), 0644)
	if err != nil {
		panic(err)
	}
}

func determineOrAskSdvxFolder() string {
	// HKEY_LOCAL_MACHINE\SOFTWARE\KONAMI\SOUND VOLTEX EXCEED GEAR\InstallDir
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\KONAMI\SOUND VOLTEX EXCEED GEAR`, registry.QUERY_VALUE)
	if err != nil {
		panic(err)
	}
	defer k.Close()

	var installDir string
	installDir, _, err = k.GetStringValue("InstallDir")
	if err != nil {
		if err != os.ErrNotExist {
			panic(err)
		}

		for {
			fmt.Print(`

We could not find your SOUND VOLTEX EXCEED GEAR install folder. Please provide it, or we
can't launch the game for you!

Provide install folder: `)

			_, err := fmt.Scanln(&installDir)
			if err != nil {
				panic(err)
			}

			_, err = os.Stat(path.Join(installDir, "launcher", "modules", "launcher.exe"))
			if err == nil {
				break
			}
			fmt.Println("ERROR: Folder does not seem like an EXCEED GEAR installation. Please try again.")
		}
	} else {
		fmt.Println("[SDVX] Autodetected game folder!")
	}

	return installDir
}

func launchSdvx(gameFolder string, token string, direct bool) {
	if direct {
		// Spawn game directly
		// TODO: Version checker to spawn launcher instead?
		exe := path.Join(gameFolder, "game", "modules", "sv6c.exe")
		exec.Command(exe, "-t", token).Start()
	} else {
		// Spawn launcher
		exe := path.Join(gameFolder, "launcher", "modules", "launcher.exe")
		arg := fmt.Sprintf("konaste.sdvx://login/?tk=%s", token)
		exec.Command(exe, arg).Start()
	}
}
