/*
Copyright 2022 The Dapr Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

const (
	appPort         = 3000
	secretKey       = "secret-key"
	secretStoreName = "local-secret-store"
	/* #nosec */
	secretURL = "http://localhost:3500/v1.0/secrets/%s/%s?metadata.namespace=dapr-tests"
)

type appResponse struct {
	Message   string `json:"message,omitempty"`
	StartTime int    `json:"start_time,omitempty"`
	EndTime   int    `json:"end_time,omitempty"`
}

// indexHandler is the handler for root path
func indexHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("indexHandler is called")

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(appResponse{Message: "OK"})
}

func volumeMountTest() (int, appResponse) {
	log.Printf("volumeMountTest is called")

	// the secret store will be only able to get the value
	// if the volume is mounted correctly.
	url, err := url.Parse(fmt.Sprintf(secretURL, secretStoreName, secretKey))
	if err != nil {
		return http.StatusInternalServerError, appResponse{Message: fmt.Sprintf("Failed to parse secret url: %v", err)}
	}

	// get the secret value
	resp, err := http.Get(url.String())
	if err != nil {
		return http.StatusInternalServerError, appResponse{Message: fmt.Sprintf("Failed to get secret: %v", err)}
	}
	defer resp.Body.Close()

	// parse the secret value
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return http.StatusInternalServerError, appResponse{Message: fmt.Sprintf("Failed to read secret: %v", err)}
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("Non 200 StatusCode: %d\n", resp.StatusCode)
		return resp.StatusCode, appResponse{Message: fmt.Sprintf("Got error response for URL %s from Dapr: %v", url.String(), string(body))}
	}

	log.Printf("Found secret value: %s\n", string(body))

	state := map[string]string{}
	err = json.Unmarshal(body, &state)
	if err != nil {
		return http.StatusInternalServerError, appResponse{Message: fmt.Sprintf("Failed to unmarshal secret: %v", err)}
	}

	return http.StatusOK, appResponse{Message: state[secretKey]}
}

// commandHandler is the handler for end-to-end test entry point
// test driver code call this endpoint to trigger the test
func commandHandler(w http.ResponseWriter, r *http.Request) {
	testCommand := mux.Vars(r)["command"]

	// Trigger the test
	res := appResponse{Message: fmt.Sprintf("%s is not supported", testCommand)}
	statusCode := http.StatusBadRequest

	startTime := epoch()
	switch testCommand {
	case "testVolumeMount":
		statusCode, res = volumeMountTest()
	}
	res.StartTime = startTime
	res.EndTime = epoch()

	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(res)
}

// epoch returns the current unix epoch timestamp
func epoch() int {
	return (int)(time.Now().UTC().UnixNano() / 1000000)
}

// appRouter initializes restful api router
func appRouter() *mux.Router {
	router := mux.NewRouter().StrictSlash(true)

	router.HandleFunc("/", indexHandler).Methods("GET")
	router.HandleFunc("/tests/{command}", commandHandler).Methods("POST")

	router.Use(mux.CORSMethodMiddleware(router))

	return router
}

func startServer() {
	// Create a server capable of supporting HTTP2 Cleartext connections
	// Also supports HTTP1.1 and upgrades from HTTP1.1 to HTTP2
	h2s := &http2.Server{}
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", appPort),
		Handler: h2c.NewHandler(appRouter(), h2s),
	}

	// Stop the server when we get a termination signal
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGKILL, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		// Wait for cancelation signal
		<-stopCh
		log.Println("Shutdown signal received")
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	// Blocking call
	err := server.ListenAndServe()
	if err != http.ErrServerClosed {
		log.Fatalf("Failed to run server: %v", err)
	}
	log.Println("Server shut down")
}

func main() {
	log.Printf("Injector App - listening on http://localhost:%d", appPort)
	startServer()
}
