package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"net/http"
	"encoding/json"
)

// json struct tags are required to parse the json field into the corresponding struct field
type AuthResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	IssuedAt    string `json:"issued_at"`
}

func fetchAuthToken(imageName string) string {

	requestURL := fmt.Sprintf("https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/%s:pull", imageName)

	response, err := http.Get(requestURL)

	if err != nil {
		fmt.Println("error making http request:", err)
		os.Exit(1)
	}

	if response.StatusCode != http.StatusOK {
		fmt.Println("Invalid http response code")
		os.Exit(1)
	}

	var authReponse AuthResponse
	err = json.NewDecoder(response.Body).Decode(&authReponse)
	if err != nil {
		fmt.Println("could not read response body:", err)
		os.Exit(1)
	}

	return authReponse.Token

}

type ImageManifest struct {
	// Commenting out unneeded ImageManifest details
	// SchemaVersion int     `json:"schemaVersion"`
	// MediaType     string  `json:"mediaType"`
	// Config        Config  `json:"config"`
	Layers        []Layer `json:"layers"`
}

type Layer struct {
	// Commenting out unneeded Layer details
	// MediaType string `json:"mediaType"`
	// Size      int    `json:"size"`
	Digest    string `json:"digest"`

}

func fetchImageManifestLayers(imageName string, reference string, authToken string) []Layer {

	requestURL := fmt.Sprintf("https://registry.hub.docker.com/v2/library/%s/manifests/%s", imageName, reference)
	// fmt.Println("Fetching Image Manifest from", requestURL)

	request, err := http.NewRequest(http.MethodGet, requestURL, nil)

	if err != nil {
		fmt.Println("error creating http request:", err)
		os.Exit(1)
	}

	request.Header.Set("Authorization", "Bearer " + authToken)
	request.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")

	response, err := http.DefaultClient.Do(request)

	if err != nil {
		fmt.Println("error making http request:", err)
		os.Exit(1)
	}

	if response.StatusCode != http.StatusOK {
		fmt.Println("Invalid http response:", response.Status)
		os.Exit(1)
	}

	var manifestResponse ImageManifest
	err = json.NewDecoder(response.Body).Decode(&manifestResponse)
	if err != nil {
		fmt.Println("could not parse manifest response body:", err)
		os.Exit(1)
	}

	return manifestResponse.Layers
}

func pullLayer(imageName string, layer Layer, authToken string) string {
	

	requestUrl := fmt.Sprintf("https://registry.hub.docker.com/v2/library/%s/blobs/%s", imageName, layer.Digest)
	request, err := http.NewRequest("GET", requestUrl, nil)
	request.Header.Set("Authorization", "Bearer " + authToken)
	if err != nil {
		fmt.Println("Failed to create request:", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		fmt.Println("Error while getting blob response: ", err)
		os.Exit(1)
	}
	defer response.Body.Close()

	// Create a new file for writing
	file, err := os.Create(fmt.Sprintf("%s.tar.gz", layer.Digest[7:]))
	if err != nil {
		fmt.Println("Error creating file: ", err)
		os.Exit(1)
	}
	defer file.Close()

	// Copy the response body to the file
	_, err = io.Copy(file, response.Body)
	if err != nil {
		fmt.Println("Error saving response to file: ", err)
		os.Exit(1)
	}

	return file.Name()
}

func applyLayer(layerArchive string, containerDir string) {

	cmd := exec.Command("tar", "-xzf", layerArchive, "-C", containerDir)
	err := cmd.Run()
	if err != nil {
		fmt.Println("Error while applying layer: ", err)
		os.Exit(1)
	}
}

func setupImage(imageWithRef string, containerDir string) error {

	imageName, reference, foundTag := strings.Cut(imageWithRef, ":")

	if(!foundTag) {
		var foundDigest bool
		imageName, reference, foundDigest = strings.Cut(imageWithRef, "@")

		if(!foundDigest) {
			imageName = imageWithRef
			reference = "latest"
		}
	}


	authToken := fetchAuthToken(imageName)
	// fmt.Println("AuthToken:", authToken)

	imageLayers := fetchImageManifestLayers(imageName, reference, authToken)
	// fmt.Println("imageLayers:", imageLayers)

	for _, layer := range(imageLayers) {
		layerArchive := pullLayer(imageName, layer, authToken)
		applyLayer(layerArchive, containerDir)
	}

	return nil
}

func isolatedRun(containerDir string, command string, inputArgs ...string) error {

	initial_args := [...]string{containerDir, command}
	args := append(initial_args[:], inputArgs...)
	cmd := exec.Command("chroot", args...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Isolate cmd in it's own process namespace
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWPID,
	}

	return cmd.Run()

}

// Usage: your_docker.sh run <image> <command> <arg1> <arg2> ...
func main() {
	imageWithRef := os.Args[2]
	// fmt.Println("imageWithRef:", imageWithRef)
	command := os.Args[3]
	args := os.Args[4:len(os.Args)]

	containerDir, mkdirErr := os.MkdirTemp("", "tempDockerRun")
	defer os.RemoveAll(containerDir)

	if mkdirErr != nil {
		fmt.Println("Error while making cage directory")
		os.Exit(1)
	}

	setupImage(imageWithRef, containerDir)
	if err := isolatedRun(containerDir, command, args...); err != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.As(err, &exitErr):
			fmt.Println("ExitErr: ", err)
			os.Exit(exitErr.ProcessState.ExitCode())
		default:
			fmt.Println("Err: ", err)
			os.Exit(1)
		}
	}
}
