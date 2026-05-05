//go:build e2e
// +build e2e

/*
Copyright 2026.

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

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/walnuts1018/cluster-api-provider-tart/test/utils"
)

// namespace where the project is deployed in
const namespace = "cluster-api-provider-tart-system"

// serviceAccountName created for the project
const serviceAccountName = "cluster-api-provider-tart-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "cluster-api-provider-tart-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "cluster-api-provider-tart-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to allow host networking for the manager")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=privileged")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace for privileged policy")

		By("installing CRDs")
		cmd = exec.Command("mise", "run", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("kubectl", "apply", "-k", "config/e2e")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		if _, err := utils.Run(cmd); err != nil {
			utils.WarnError(err)
		}

		By("undeploying the controller-manager")
		cmd = exec.Command("kubectl", "delete", "--ignore-not-found=true", "-k", "config/e2e")
		if _, err := utils.Run(cmd); err != nil {
			utils.WarnError(err)
		}

		By("uninstalling CRDs")
		cmd = exec.Command("mise", "run", "uninstall")
		cmd.Env = append(os.Environ(), "IGNORE_NOT_FOUND=true")
		if _, err := utils.Run(cmd); err != nil {
			utils.WarnError(err)
		}

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace, "--ignore-not-found=true")
		if _, err := utils.Run(cmd); err != nil {
			utils.WarnError(err)
		}
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				if _, writeErr := fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs); writeErr != nil {
					log.Printf("failed to write controller logs to GinkgoWriter: %v", writeErr)
				}
			} else {
				if _, writeErr := fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err); writeErr != nil {
					log.Printf("failed to write controller log error to GinkgoWriter: %v", writeErr)
				}
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				if _, writeErr := fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput); writeErr != nil {
					log.Printf("failed to write Kubernetes events to GinkgoWriter: %v", writeErr)
				}
			} else {
				if _, writeErr := fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err); writeErr != nil {
					log.Printf("failed to write Kubernetes event error to GinkgoWriter: %v", writeErr)
				}
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				if _, writeErr := fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput); writeErr != nil {
					log.Printf("failed to write metrics logs to GinkgoWriter: %v", writeErr)
				}
			} else {
				if _, writeErr := fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err); writeErr != nil {
					log.Printf("failed to write metrics log error to GinkgoWriter: %v", writeErr)
				}
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("removing a stale ClusterRoleBinding for metrics")
			cmd := exec.Command("kubectl", "delete", "clusterrolebinding", metricsRoleBindingName, "--ignore-not-found")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete stale ClusterRoleBinding")

			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd = exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=cluster-api-provider-tart-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("ensuring the controller pod is ready")
			verifyControllerPodReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Controller pod not ready")
			}
			Eventually(verifyControllerPodReady, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted, 3*time.Minute, time.Second).Should(Succeed())

			// +kubebuilder:scaffold:e2e-metrics-webhooks-readiness

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": [
								"for i in $(seq 1 30); do curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics && exit 0 || sleep 2; done; exit 1"
							],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks

		It("should accept multi OS TartMachineTemplate samples", func() {
			tests := []struct {
				name       string
				file       string
				resource   string
				wantFormat string
			}{
				{
					name:       "standalone Ubuntu NoCloud TartMachineTemplate",
					file:       "config/samples/infrastructure_v1alpha1_tartmachinetemplate.yaml",
					resource:   "tartmachinetemplate-sample",
					wantFormat: "NoCloud",
				},
				{
					name:       "kubeadm Ubuntu sample",
					file:       "config/samples/cluster-kubeadm-ubuntu.yaml",
					resource:   "tart-kubeadm-ubuntu-control-plane",
					wantFormat: "NoCloud",
				},
				{
					name:       "kubeadm Debian sample",
					file:       "config/samples/cluster-kubeadm-debian.yaml",
					resource:   "tart-kubeadm-debian-control-plane",
					wantFormat: "Preseed",
				},
				{
					name:       "k3s Ubuntu sample",
					file:       "config/samples/cluster-k3s-ubuntu.yaml",
					resource:   "tart-k3s-ubuntu-control-plane",
					wantFormat: "NoCloud",
				},
				{
					name:       "k3s Debian sample",
					file:       "config/samples/cluster-k3s-debian.yaml",
					resource:   "tart-k3s-debian-control-plane",
					wantFormat: "Preseed",
				},
				{
					name:       "Talos sample",
					file:       "config/samples/cluster-talos.yaml",
					resource:   "tart-talos-control-plane",
					wantFormat: "Talos",
				},
			}

			DeferCleanup(func() {
				for i := len(tests) - 1; i >= 0; i-- {
					tt := tests[i]
					By("deleting " + tt.name)
					cmd := exec.Command("kubectl", "delete",
						"-n", namespace,
						"-f", tt.file,
						"--ignore-not-found",
					)
					_, _ = utils.Run(cmd)
				}
			})

			for _, tt := range tests {
				By("applying " + tt.name)
				cmd := exec.Command("kubectl", "apply",
					"-n", namespace,
					"-f", tt.file,
				)
				_, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(), "Failed to apply "+tt.name+" from "+tt.file)

				By("validating bootstrap format for " + tt.name)
				cmd = exec.Command("kubectl", "get", "tartmachinetemplate", tt.resource,
					"-n", namespace,
					"-o", "jsonpath={.spec.template.spec.bootstrap.format}",
				)
				output, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(), "Failed to get "+tt.resource)
				Expect(output).To(Equal(tt.wantFormat))
			}
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
