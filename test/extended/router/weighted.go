package router

import (
	"context"
	"encoding/csv"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	g "github.com/onsi/ginkgo"
	o "github.com/onsi/gomega"

	e2e "k8s.io/kubernetes/test/e2e/framework"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/kubernetes/test/e2e/framework/pod"
	admissionapi "k8s.io/pod-security-admission/api"

	exutil "github.com/openshift/origin/test/extended/util"
)

var _ = g.Describe("[sig-network][Feature:Router][apigroup:config.openshift.io][apigroup:image.openshift.io]", func() {
	defer g.GinkgoRecover()
	var (
		configPath = exutil.FixturePath("testdata", "router", "weighted-router.yaml")
		oc         = exutil.NewCLIWithPodSecurityLevel("weighted-router", admissionapi.LevelBaseline)
	)

	g.BeforeEach(func() {
		routerImage, err := exutil.FindRouterImage(oc)
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().Run("new-app").Args("-f", configPath, "-p", "IMAGE="+routerImage).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.Describe("The HAProxy router", func() {
		g.It("should serve a route that points to two services and respect weights", func() {
			defer func() {
				if g.CurrentGinkgoTestDescription().Failed {
					dumpWeightedRouterLogs(oc, g.CurrentGinkgoTestDescription().FullTestText)
				}
			}()

			ns := oc.KubeFramework().Namespace.Name
			execPod := exutil.CreateExecPodOrFail(oc.AdminKubeClient(), ns, "execpod")
			defer func() {
				oc.AdminKubeClient().CoreV1().Pods(ns).Delete(context.Background(), execPod.Name, *metav1.NewDeleteOptions(1))
			}()

			g.By(fmt.Sprintf("creating a weighted router from a config file %q", configPath))

			var routerIP string
			err := wait.Poll(time.Second, changeTimeoutSeconds*time.Second, func() (bool, error) {
				pod, err := oc.KubeFramework().ClientSet.CoreV1().Pods(oc.KubeFramework().Namespace.Name).Get(context.Background(), "weighted-router", metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if len(pod.Status.PodIP) == 0 {
					return false, nil
				}

				routerIP = pod.Status.PodIP
				return true, nil
			})
			o.Expect(err).NotTo(o.HaveOccurred())

			// router expected to listen on port 80
			routerURL := fmt.Sprintf("http://%s", routerIP)

			g.By("waiting for the healthz endpoint to respond")
			healthzURI := fmt.Sprintf("http://%s/healthz", net.JoinHostPort(routerIP, "1936"))
			err = waitForRouterOKResponseExec(ns, execPod.Name, healthzURI, routerIP, changeTimeoutSeconds)
			o.Expect(err).NotTo(o.HaveOccurred())

			host := "weighted.example.com"
			times := 100
			g.By(fmt.Sprintf("checking that %d requests go through successfully", times))
			// wait for the request to stabilize
			err = waitForRouterOKResponseExec(ns, execPod.Name, routerURL, "weighted.example.com", changeTimeoutSeconds)
			o.Expect(err).NotTo(o.HaveOccurred())
			// all requests should now succeed
			err = expectRouteStatusCodeRepeatedExec(ns, execPod.Name, routerURL, "weighted.example.com", http.StatusOK, times, false)
			o.Expect(err).NotTo(o.HaveOccurred())

			g.By(fmt.Sprintf("checking that there are three weighted backends in the router stats"))
			var trafficValues []string
			err = wait.PollImmediate(100*time.Millisecond, changeTimeoutSeconds*time.Second, func() (bool, error) {
				statsURL := fmt.Sprintf("http://%s/;csv", net.JoinHostPort(routerIP, "1936"))
				stats, err := getAuthenticatedRouteURLViaPod(ns, execPod.Name, statsURL, host, "admin", "password")
				o.Expect(err).NotTo(o.HaveOccurred())
				trafficValues, err = parseStats(stats, "weightedroute", 7)
				o.Expect(err).NotTo(o.HaveOccurred())
				return len(trafficValues) == 3, nil
			})
			o.Expect(err).NotTo(o.HaveOccurred())

			trafficEP1, err := strconv.Atoi(trafficValues[0])
			o.Expect(err).NotTo(o.HaveOccurred())
			trafficEP2, err := strconv.Atoi(trafficValues[1])
			o.Expect(err).NotTo(o.HaveOccurred())

			weightedRatio := float32(trafficEP1) / float32(trafficEP2)
			if weightedRatio < 5 && weightedRatio > 0.2 {
				e2e.Failf("Unexpected weighted ratio for incoming traffic: %v (%d/%d)", weightedRatio, trafficEP1, trafficEP2)
			}

			g.By(fmt.Sprintf("checking that zero weights are also respected by the router"))
			host = "zeroweight.example.com"
			err = expectRouteStatusCodeExec(ns, execPod.Name, routerURL, host, http.StatusServiceUnavailable)
			o.Expect(err).NotTo(o.HaveOccurred())
		})
	})
})

func parseStats(stats string, backendSubstr string, statsField int) ([]string, error) {
	r := csv.NewReader(strings.NewReader(stats))
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	fieldValues := make([]string, 0)
	for _, rec := range records {
		if strings.Contains(rec[0], backendSubstr) && !strings.Contains(rec[1], "BACKEND") {
			fieldValues = append(fieldValues, rec[statsField])
		}
	}
	return fieldValues, nil
}

func dumpWeightedRouterLogs(oc *exutil.CLI, name string) {
	log, _ := pod.GetPodLogs(oc.AdminKubeClient(), oc.KubeFramework().Namespace.Name, "weighted-router", "router")
	e2e.Logf("Weighted Router test %s logs:\n %s", name, log)
}
