package cli

import (
	"fmt"
	"time"

	g "github.com/onsi/ginkgo"
	o "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/util/wait"
	admissionapi "k8s.io/pod-security-admission/api"

	exutil "github.com/openshift/origin/test/extended/util"
)

var (
	buildTimeout  = 10 * time.Minute
	deployTimeout = 2 * time.Minute
)

var _ = g.Describe("[sig-cli] oc debug", func() {
	defer g.GinkgoRecover()

	oc := exutil.NewCLIWithPodSecurityLevel("oc-debug", admissionapi.LevelBaseline)
	testCLIDebug := exutil.FixturePath("testdata", "test-cli-debug.yaml")
	testDeploymentConfig := exutil.FixturePath("testdata", "test-deployment-config.yaml")
	testReplicationController := exutil.FixturePath("testdata", "test-replication-controller.yaml")
	helloPod := exutil.FixturePath("..", "..", "examples", "hello-openshift", "hello-pod.json")
	imageStreamsCentos := exutil.FixturePath("..", "..", "examples", "image-streams", "image-streams-centos7.json")

	g.It("deployment configs from a build [apigroup:image.openshift.io][apigroup:apps.openshift.io]", func() {
		err := oc.Run("create").Args("-f", testCLIDebug).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		// wait for image stream to be present which means the build has completed
		err = wait.Poll(cliInterval, buildTimeout, func() (bool, error) {
			err := oc.Run("get").Args("imagestreamtags", "local-busybox:latest").Execute()
			return err == nil, nil
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		// and for replication controller which means we can kick of debug session
		err = wait.Poll(cliInterval, deployTimeout, func() (bool, error) {
			err := oc.Run("get").Args("replicationcontrollers", "local-busybox1-1").Execute()
			return err == nil, nil
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("should print the imagestream-based container entrypoint/command")
		var out string
		out, err = oc.Run("debug").Args("dc/local-busybox1").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("Starting pod/local-busybox1-debug, command was: /usr/bin/bash\n"))

		g.By("should print the overridden imagestream-based container entrypoint/command")
		out, err = oc.Run("debug").Args("dc/local-busybox2").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("Starting pod/local-busybox2-debug, command was: foo bar baz qux\n"))

		g.By("should print the container image-based container entrypoint/command")
		out, err = oc.Run("debug").Args("dc/busybox1").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("Starting pod/busybox1-debug ...\n"))

		g.By("should print the overridden container image-based container entrypoint/command")
		out, err = oc.Run("debug").Args("dc/busybox2").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("Starting pod/busybox2-debug, command was: foo bar baz qux\n"))
	})

	g.It("dissect deployment config debug [apigroup:apps.openshift.io]", func() {
		err := oc.Run("create").Args("-f", testDeploymentConfig).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		var out string
		out, err = oc.Run("debug").Args("dc/test-deployment-config", "-oyaml").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("- /bin/sh"))

		out, err = oc.Run("debug").Args("dc/test-deployment-config", "--keep-annotations", "-oyaml").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("annotations:"))

		out, err = oc.Run("debug").Args("dc/test-deployment-config", "--as-root", "-oyaml").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("runAsUser: 0"))

		out, err = oc.Run("debug").Args("dc/test-deployment-config", "--as-root=false", "-oyaml").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("runAsNonRoot: true"))

		out, err = oc.Run("debug").Args("dc/test-deployment-config", "--as-user=1", "-oyaml").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("runAsUser: 1"))

		out, err = oc.Run("debug").Args("dc/test-deployment-config", "-t", "-oyaml").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("stdinOnce"))
		o.Expect(out).To(o.ContainSubstring("tty"))

		out, err = oc.Run("debug").Args("dc/test-deployment-config", "--tty=false", "-oyaml").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).NotTo(o.ContainSubstring("tty"))

		out, err = oc.Run("debug").Args("dc/test-deployment-config", "-oyaml", "--", "/bin/env").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("- /bin/env"))
		o.Expect(out).NotTo(o.ContainSubstring("stdin"))
		o.Expect(out).NotTo(o.ContainSubstring("tty"))

		out, err = oc.Run("debug").Args("dc/test-deployment-config", "--node-name=invalid", "--", "/bin/env").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring(`on node "invalid"`))
	})

	g.It("does not require a real resource on the server", func() {
		out, err := oc.Run("debug").Args("-T", "-f", helloPod, "-oyaml").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).NotTo(o.ContainSubstring("tty"))

		err = oc.Run("debug").Args("-f", helloPod, "--keep-liveness", "--keep-readiness", "-oyaml").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		out, err = oc.Run("debug").Args("-f", helloPod, "-oyaml", "--", "/bin/env").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("- /bin/env"))
		o.Expect(out).NotTo(o.ContainSubstring("stdin"))
		o.Expect(out).NotTo(o.ContainSubstring("tty"))
	})

	// TODO: write a test that emulates a TTY to verify the correct defaulting of what the pod is created

	g.It("ensure debug does not depend on a container actually existing for the selected resource [apigroup:apps.openshift.io]", func() {
		err := oc.Run("create").Args("-f", testReplicationController).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.Run("create").Args("-f", testDeploymentConfig).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		// The command should not hang waiting for an attachable pod. Timeout each cmd after 10s.
		err = oc.Run("scale").Args("--replicas=0", "rc/test-replication-controller").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		var out string
		out, err = oc.Run("debug").Args("--request-timeout=10s", "-c", "ruby-helloworld", "--one-container", "rc/test-replication-controller", "-o", "jsonpath='{.metadata.name}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("test-replication-controller-debug"))

		err = oc.Run("scale").Args("--replicas=0", "dc/test-deployment-config").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		out, err = oc.Run("debug").Args("--request-timeout=10s", "-c", "ruby-helloworld", "--one-container", "dc/test-deployment-config", "-o", "jsonpath='{.metadata.name}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("test-deployment-config"))

		err = oc.Run("create").Args("-f", "-").InputString(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
  labels:
    deployment: test-deployment
spec:
  replicas: 0
  selector:
    matchLabels:
      deployment: test-deployment
  template:
    metadata:
      labels:
        deployment: test-deployment
      name: test-deployment
    spec:
      containers:
      - name: ruby-helloworld
        image: openshift/origin-pod
        imagePullPolicy: IfNotPresent
`).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		out, err = oc.Run("debug").Args("--request-timeout=10s", "-c", "ruby-helloworld", "--one-container", "deploy/test-deployment", "-o", "jsonpath='{.metadata.name}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("test-deployment-debug"))
	})

	g.It("ensure it works with image streams [apigroup:image.openshift.io]", func() {
		err := oc.Run("create").Args("-f", imageStreamsCentos).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = wait.Poll(cliInterval, cliTimeout, func() (bool, error) {
			err := oc.Run("get").Args("imagestreamtags", "wildfly:latest").Execute()
			return err == nil, nil
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		var out string
		out, err = oc.Run("debug").Args("istag/wildfly:latest", "-o", "yaml").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.MatchRegexp("image:.*oc-debug-.*/wildfly@sha256"))

		var sha string
		sha, err = oc.Run("get").Args("istag/wildfly:latest", "--template", "{{ .image.metadata.name }}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		out, err = oc.Run("debug").Args(fmt.Sprintf("isimage/wildfly@%s", sha), "-o", "yaml").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(out).To(o.ContainSubstring("image: quay.io/wildfly/wildfly-centos7"))
	})
})
