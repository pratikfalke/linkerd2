package test

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/testutil"
	corev1 "k8s.io/api/core/v1"
)

type deploySpec struct {
	replicas   int
	containers []string
}

//////////////////////
///   TEST SETUP   ///
//////////////////////

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

var (
	configMapUID string

	helmTLSCerts *tls.CA

	linkerdSvcs = []string{
		"linkerd-controller-api",
		"linkerd-dst",
		"linkerd-grafana",
		"linkerd-identity",
		"linkerd-prometheus",
		"linkerd-web",
		"linkerd-tap",
	}

	linkerdDeployReplicas = map[string]deploySpec{
		"linkerd-controller":     {1, []string{"public-api"}},
		"linkerd-destination":    {1, []string{"destination"}},
		"linkerd-tap":            {1, []string{"tap"}},
		"linkerd-grafana":        {1, []string{}},
		"linkerd-identity":       {1, []string{"identity"}},
		"linkerd-prometheus":     {1, []string{}},
		"linkerd-sp-validator":   {1, []string{"sp-validator"}},
		"linkerd-web":            {1, []string{"web"}},
		"linkerd-proxy-injector": {1, []string{"proxy-injector"}},
	}

	// Linkerd commonly logs these errors during testing, remove these once
	// they're addressed: https://github.com/linkerd/linkerd2/issues/2453
	knownControllerErrorsRegex = regexp.MustCompile(strings.Join([]string{
		`.*linkerd-controller-.*-.* tap time=".*" level=error msg="\[.*\] encountered an error: rpc error: code = Canceled desc = context canceled"`,
		`.*linkerd-web-.*-.* web time=".*" level=error msg="Post http://linkerd-controller-api\..*\.svc\.cluster\.local:8085/api/v1/Version: context canceled"`,
		`.*linkerd-proxy-injector-.*-.* proxy-injector time=".*" level=warning msg="failed to retrieve replicaset from indexer .*-smoke-test.*/smoke-test-.*-.*: replicaset\.apps \\"smoke-test-.*-.*\\" not found"`,
		`.*linkerd-destination-.* destination time=".*" level=warning msg="failed to retrieve replicaset from indexer .* not found"`,
	}, "|"))

	knownProxyErrorsRegex = regexp.MustCompile(strings.Join([]string{
		// k8s hitting readiness endpoints before components are ready
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web|tap)-.*-.* linkerd-proxy ERR! \[ +\d+.\d+s\] proxy={server=in listen=0\.0\.0\.0:4143 remote=.*} linkerd2_proxy::app::errors unexpected error: an IO error occurred: Connection reset by peer \(os error 104\)`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web|tap)-.*-.* linkerd-proxy ERR! \[ *\d+.\d+s\] proxy={server=in listen=0\.0\.0\.0:4143 remote=.*} linkerd2_proxy::(proxy::http::router service|app::errors unexpected) error: an error occurred trying to connect: Connection refused \(os error 111\) \(address: 127\.0\.0\.1:.*\)`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web|tap)-.*-.* linkerd-proxy ERR! \[ *\d+.\d+s\] proxy={server=out listen=127\.0\.0\.1:4140 remote=.*} linkerd2_proxy::(proxy::http::router service|app::errors unexpected) error: an error occurred trying to connect: Connection refused \(os error 111\) \(address: .*\)`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web|tap)-.*-.* linkerd-proxy ERR! \[ *\d+.\d+s\] proxy={server=out listen=127\.0\.0\.1:4140 remote=.*} linkerd2_proxy::(proxy::http::router service|app::errors unexpected) error: an error occurred trying to connect: operation timed out after 1s`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web|tap)-.*-.* linkerd-proxy WARN \[ *\d+.\d+s\] .* linkerd2_proxy::proxy::reconnect connect error to ControlAddr .*`,

		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web|tap)-.*-.* linkerd-proxy ERR! \[ *\d+.\d+s\] admin={server=metrics listen=0\.0\.0\.0:4191 remote=.*} linkerd2_proxy::control::serve_http error serving metrics: Error { kind: Shutdown, .* }`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web|tap)-.*-.* linkerd-proxy ERR! \[ +\d+.\d+s\] admin={server=admin listen=127\.0\.0\.1:4191 remote=.*} linkerd2_proxy::control::serve_http error serving admin: Error { kind: Shutdown, cause: Os { code: 107, kind: NotConnected, message: "Transport endpoint is not connected" } }`,

		`.* linkerd-web-.*-.* linkerd-proxy WARN trust_dns_proto::xfer::dns_exchange failed to associate send_message response to the sender`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|web|tap)-.*-.* linkerd-proxy WARN \[.*\] linkerd2_proxy::proxy::canonicalize failed to refine linkerd-.*\..*\.svc\.cluster\.local: deadline has elapsed; using original name`,

		// prometheus scrape failures of control-plane
		`.* linkerd-prometheus-.*-.* linkerd-proxy ERR! \[ +\d+.\d+s\] proxy={server=out listen=127\.0\.0\.1:4140 remote=.*} linkerd2_proxy::proxy::http::router service error: an error occurred trying to connect: .*`,
	}, "|"))

	knownEventWarningsRegex = regexp.MustCompile(strings.Join([]string{
		`MountVolume.SetUp failed for volume .* : couldn't propagate object cache: timed out waiting for the condition`, // pre k8s 1.16
		`MountVolume.SetUp failed for volume .* : failed to sync .* cache: timed out waiting for the condition`,         // post k8s 1.16
		`(Liveness|Readiness) probe failed: HTTP probe failed with statuscode: 50(2|3)`,
		`(Liveness|Readiness) probe failed: Get http://.*: dial tcp .*: connect: connection refused`,
		`(Liveness|Readiness) probe failed: Get http://.*: read tcp .*: read: connection reset by peer`,
		`(Liveness|Readiness) probe failed: Get http://.*: net/http: request canceled .*\(Client\.Timeout exceeded while awaiting headers\)`,
		`Failed to update endpoint .*-upgrade/linkerd-.*: Operation cannot be fulfilled on endpoints "linkerd-.*": the object has been modified; please apply your changes to the latest version and try again`,
		`error killing pod: failed to "KillPodSandbox" for ".*" with KillPodSandboxError: "rpc error: code = Unknown desc = failed to destroy network for sandbox \\".*\\": could not teardown (ipv4|ipv6) dnat: running \[/usr/sbin/(iptables|ip6tables) -t nat -X CNI-DN-.* --wait\]: exit status 1: (iptables|ip6tables): No chain/target/match by that name\.\\n"`,
	}, "|"))

	injectionCases = []struct {
		ns          string
		annotations map[string]string
		injectArgs  []string
	}{
		{
			ns: "smoke-test",
			annotations: map[string]string{
				k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
			},
			injectArgs: nil,
		},
		{
			ns:         "smoke-test-manual",
			injectArgs: []string{"--manual"},
		},
		{
			ns:         "smoke-test-ann",
			injectArgs: []string{},
		},
	}
)

//////////////////////
/// TEST EXECUTION ///
//////////////////////

// Tests are executed in serial in the order defined
// Later tests depend on the success of earlier tests

func TestVersionPreInstall(t *testing.T) {
	version := "unavailable"
	if TestHelper.UpgradeFromVersion() != "" {
		version = TestHelper.UpgradeFromVersion()
	}

	err := TestHelper.CheckVersion(version)
	if err != nil {
		t.Fatalf("Version command failed\n%s", err.Error())
	}
}

func TestCheckPreInstall(t *testing.T) {
	if TestHelper.ExternalIssuer() {
		t.Skip("Skipping pre-install check for external issuer test")
	}

	if TestHelper.UpgradeFromVersion() != "" {
		t.Skip("Skipping pre-install check for upgrade test")
	}

	cmd := []string{"check", "--pre", "--expected-version", TestHelper.GetVersion()}
	golden := "check.pre.golden"
	out, stderr, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("Check command failed\n%s\n%s", out, stderr)
	}

	err = TestHelper.ValidateOutput(out, golden)
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err.Error())
	}
}

func exerciseTestAppEndpoint(endpoint, namespace string) error {
	testAppURL, err := TestHelper.URLFor(namespace, "web", 8080)
	if err != nil {
		return err
	}
	for i := 0; i < 30; i++ {
		_, err := TestHelper.HTTPGetURL(testAppURL + endpoint)
		if err != nil {
			return err
		}
	}
	return nil
}

func TestUpgradeTestAppWorksBeforeUpgrade(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
		// make sure app is running
		testAppNamespace := TestHelper.GetTestNamespace("upgrade-test")
		for _, deploy := range []string{"emoji", "voting", "web"} {
			if err := TestHelper.CheckPods(testAppNamespace, deploy, 1); err != nil {
				t.Error(err)
			}

			if err := TestHelper.CheckDeployment(testAppNamespace, deploy, 1); err != nil {
				t.Error(fmt.Errorf("Error validating deployment [%s]:\n%s", deploy, err))
			}
		}

		if err := exerciseTestAppEndpoint("/api/list", testAppNamespace); err != nil {
			t.Fatalf("Error exercising test app endpoint before upgrade %s", err)
		}
	} else {
		t.Skip("Skipping for non upgrade test")
	}
}

func TestRetrieveUidPreUpgrade(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
		var err error
		configMapUID, err = TestHelper.KubernetesHelper.GetConfigUID(TestHelper.GetLinkerdNamespace())
		if err != nil || configMapUID == "" {
			t.Fatalf("Error retrieving linkerd-config's uid %s", err)
		}
	}
}

func TestInstallOrUpgradeCli(t *testing.T) {
	if TestHelper.GetHelmReleaseName() != "" {
		return
	}

	var (
		cmd  = "install"
		args = []string{
			"--controller-log-level", "debug",
			"--proxy-log-level", "warn,linkerd2_proxy=debug",
			"--proxy-version", TestHelper.GetVersion(),
		}
	)

	if TestHelper.GetClusterDomain() != "cluster.local" {
		args = append(args, "--cluster-domain", TestHelper.GetClusterDomain())
	}

	if TestHelper.ExternalIssuer() {

		// short cert lifetime to put some pressure on the CSR request, response code path
		args = append(args, "--identity-issuance-lifetime=15s", "--identity-external-issuer=true")

		err := TestHelper.CreateControlPlaneNamespaceIfNotExists(TestHelper.GetLinkerdNamespace())
		if err != nil {
			t.Fatalf("failed to create %s namespace: %s", TestHelper.GetLinkerdNamespace(), err)
		}

		identity := fmt.Sprintf("identity.%s.%s", TestHelper.GetLinkerdNamespace(), TestHelper.GetClusterDomain())

		root, err := tls.GenerateRootCAWithDefaults(identity)
		if err != nil {
			t.Fatal(err)
		}

		// instead of passing the roots and key around we generate
		// two secrets here. The second one will be used in the
		// external_issuer_test to update the first one and trigger
		// cert rotation in the identity service. That allows us
		// to generated the certs on the fly and use custom domain.

		if err = TestHelper.CreateTLSSecret(
			k8s.IdentityIssuerSecretName,
			root.Cred.Crt.EncodeCertificatePEM(),
			root.Cred.Crt.EncodeCertificatePEM(),
			root.Cred.EncodePrivateKeyPEM()); err != nil {
			t.Fatal(err)
		}

		crt2, err := root.GenerateCA(identity, -1)
		if err != nil {
			t.Fatal(err)
		}

		if err = TestHelper.CreateTLSSecret(
			k8s.IdentityIssuerSecretName+"-new",
			root.Cred.Crt.EncodeCertificatePEM(),
			crt2.Cred.EncodeCertificatePEM(),
			crt2.Cred.EncodePrivateKeyPEM()); err != nil {
			t.Fatal(err)
		}
	}

	if TestHelper.UpgradeFromVersion() != "" {

		cmd = "upgrade"
		// test 2-stage install during upgrade
		out, stderr, err := TestHelper.LinkerdRun(cmd, "config")
		if err != nil {
			t.Fatalf("linkerd upgrade config command failed\n%s\n%s", out, stderr)
		}

		// apply stage 1
		out, err = TestHelper.KubectlApply(out, "")
		if err != nil {
			t.Fatalf("kubectl apply command failed\n%s", out)
		}

		// prepare for stage 2
		args = append([]string{"control-plane"}, args...)
	}

	exec := append([]string{cmd}, args...)
	out, stderr, err := TestHelper.LinkerdRun(exec...)
	if err != nil {
		t.Fatalf("linkerd install command failed: \n%s\n%s", out, stderr)
	}

	// test `linkerd upgrade --from-manifests`
	if TestHelper.UpgradeFromVersion() != "" {
		manifests, err := TestHelper.Kubectl("",
			"--namespace", TestHelper.GetLinkerdNamespace(),
			"get", "configmaps/"+k8s.ConfigConfigMapName, "secrets/"+k8s.IdentityIssuerSecretName,
			"-oyaml",
		)
		if err != nil {
			t.Fatalf("kubectl get command failed with %s\n%s", err, out)
		}
		exec = append(exec, "--from-manifests", "-")
		upgradeFromManifests, stderr, err := TestHelper.PipeToLinkerdRun(manifests, exec...)
		if err != nil {
			t.Fatalf("linkerd upgrade --from-manifests command failed with %s\n%s\n%s", err, stderr, upgradeFromManifests)
		}

		if out != upgradeFromManifests {
			// retry in case it's just a discrepancy in the heartbeat cron schedule
			exec := append([]string{cmd}, args...)
			out, stderr, err := TestHelper.LinkerdRun(exec...)
			if err != nil {
				t.Fatalf("command failed: %v\n%s\n%s", exec, out, stderr)
			}

			if out != upgradeFromManifests {
				t.Fatalf("manifest upgrade differs from k8s upgrade.\nk8s upgrade:\n%s\nmanifest upgrade:\n%s", out, upgradeFromManifests)
			}
		}
	}

	out, err = TestHelper.KubectlApply(out, "")
	if err != nil {
		t.Fatalf("kubectl apply command failed\n%s", out)
	}

}

// These need to be updated (if there are changes) once a new stable is released
func helmOverridesStable(root *tls.CA) []string {
	return []string{
		"--set", "Namespace=" + TestHelper.GetLinkerdNamespace(),
		"--set", "ControllerLogLevel=debug",
		"--set", "LinkerdVersion=" + TestHelper.UpgradeHelmFromVersion(),
		"--set", "Proxy.Image.Version=" + TestHelper.UpgradeHelmFromVersion(),
		"--set", "Identity.TrustDomain=cluster.local",
		"--set", "Identity.TrustAnchorsPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "Identity.Issuer.TLS.CrtPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "Identity.Issuer.TLS.KeyPEM=" + root.Cred.EncodePrivateKeyPEM(),
		"--set", "Identity.Issuer.CrtExpiry=" + root.Cred.Crt.Certificate.NotAfter.Format(time.RFC3339),
	}
}

// These need to correspond to the flags in the current edge
func helmOverridesEdge(root *tls.CA) []string {
	return []string{
		"--set", "controllerLogLevel=debug",
		"--set", "global.linkerdVersion=" + TestHelper.GetVersion(),
		"--set", "global.proxy.image.version=" + TestHelper.GetVersion(),
		"--set", "global.identityTrustDomain=cluster.local",
		"--set", "global.identityTrustAnchorsPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "identity.issuer.tls.crtPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "identity.issuer.tls.keyPEM=" + root.Cred.EncodePrivateKeyPEM(),
		"--set", "identity.issuer.crtExpiry=" + root.Cred.Crt.Certificate.NotAfter.Format(time.RFC3339),
	}
}

func TestInstallHelm(t *testing.T) {
	if TestHelper.GetHelmReleaseName() == "" {
		return
	}

	cn := fmt.Sprintf("identity.%s.cluster.local", TestHelper.GetLinkerdNamespace())
	var err error
	helmTLSCerts, err = tls.GenerateRootCAWithDefaults(cn)
	if err != nil {
		t.Fatalf("failed to generate root certificate for identity: %s", err)
	}

	var chartToInstall string
	args := []string{"--name", TestHelper.GetHelmReleaseName()}

	if TestHelper.UpgradeHelmFromVersion() != "" {
		chartToInstall = TestHelper.GetHelmStableChart()
		args = append(args, helmOverridesStable(helmTLSCerts)...)
	} else {
		chartToInstall = TestHelper.GetHelmChart()
		args = append(args, helmOverridesEdge(helmTLSCerts)...)
	}

	if stdout, stderr, err := TestHelper.HelmInstall(chartToInstall, args...); err != nil {
		t.Fatalf("helm install command failed\n%s\n%s", stdout, stderr)
	}
}

func TestResourcesPostInstall(t *testing.T) {
	// Tests Namespace
	err := TestHelper.CheckIfNamespaceExists(TestHelper.GetLinkerdNamespace())
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err.Error())
	}

	// Tests Services
	for _, svc := range linkerdSvcs {
		if err := TestHelper.CheckService(TestHelper.GetLinkerdNamespace(), svc); err != nil {
			t.Error(fmt.Errorf("Error validating service [%s]:\n%s", svc, err))
		}
	}

	// Tests Pods and Deployments
	for deploy, spec := range linkerdDeployReplicas {
		if err := TestHelper.CheckPods(TestHelper.GetLinkerdNamespace(), deploy, spec.replicas); err != nil {
			t.Fatal(fmt.Errorf("Error validating pods for deploy [%s]:\n%s", deploy, err))
		}
		if err := TestHelper.CheckDeployment(TestHelper.GetLinkerdNamespace(), deploy, spec.replicas); err != nil {
			t.Fatal(fmt.Errorf("Error validating deploy [%s]:\n%s", deploy, err))
		}
	}
}

func TestCheckHelmStableBeforeUpgrade(t *testing.T) {
	if TestHelper.UpgradeHelmFromVersion() == "" {
		t.Skip("Skipping as this is not a helm upgrade test")
	}
	testCheckCommand(t, "", TestHelper.UpgradeHelmFromVersion(), "", TestHelper.UpgradeHelmFromVersion())
}

func TestUpgradeHelm(t *testing.T) {
	if TestHelper.UpgradeHelmFromVersion() == "" {
		t.Skip("Skipping as this is not a helm upgrade test")
	}

	args := []string{
		"--reset-values",
		"--atomic",
		"--wait",
	}
	args = append(args, helmOverridesEdge(helmTLSCerts)...)
	if stdout, stderr, err := TestHelper.HelmUpgrade(TestHelper.GetHelmChart(), args...); err != nil {
		t.Fatalf("helm upgrade command failed\n%s\n%s", stdout, stderr)
	}
}

func TestRetrieveUidPostUpgrade(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
		newConfigMapUID, err := TestHelper.KubernetesHelper.GetConfigUID(TestHelper.GetLinkerdNamespace())
		if err != nil || newConfigMapUID == "" {
			t.Fatalf("Error retrieving linkerd-config's uid %s", err)
		}
		if configMapUID != newConfigMapUID {
			t.Fatalf(
				"linkerd-config's uid after upgrade [%s] doesn't match its value before the upgrade [%s]",
				newConfigMapUID, configMapUID,
			)
		}
	}
}

func TestVersionPostInstall(t *testing.T) {
	err := TestHelper.CheckVersion(TestHelper.GetVersion())
	if err != nil {
		t.Fatalf("Version command failed\n%s", err.Error())
	}
}

func testCheckCommand(t *testing.T, stage string, expectedVersion string, namespace string, cliVersionOverride string) {
	var cmd []string
	var golden string
	if stage == "proxy" {
		cmd = []string{"check", "--proxy", "--expected-version", expectedVersion, "--namespace", namespace, "--wait=0"}
		golden = "check.proxy.golden"
	} else if stage == "config" {
		cmd = []string{"check", "config", "--expected-version", expectedVersion, "--wait=0"}
		golden = "check.config.golden"
	} else {
		cmd = []string{"check", "--expected-version", expectedVersion, "--wait=0"}
		golden = "check.golden"
	}

	err := TestHelper.RetryFor(time.Minute, func() error {
		if cliVersionOverride != "" {
			cliVOverride := []string{"--cli-version-override", cliVersionOverride}
			cmd = append(cmd, cliVOverride...)
		}
		out, stderr, err := TestHelper.LinkerdRun(cmd...)

		if err != nil {
			return fmt.Errorf("Check command failed\n%s\n%s", stderr, out)
		}

		err = TestHelper.ValidateOutput(out, golden)
		if err != nil {
			return fmt.Errorf("Received unexpected output\n%s", err.Error())
		}

		return nil
	})
	if err != nil {
		t.Fatal(err.Error())
	}
}

// TODO: run this after a `linkerd install config`
func TestCheckConfigPostInstall(t *testing.T) {
	testCheckCommand(t, "config", TestHelper.GetVersion(), "", "")
}

func TestCheckPostInstall(t *testing.T) {
	testCheckCommand(t, "", TestHelper.GetVersion(), "", "")
}

func TestUpgradeTestAppWorksAfterUpgrade(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
		testAppNamespace := TestHelper.GetTestNamespace("upgrade-test")
		if err := exerciseTestAppEndpoint("/api/vote?choice=:policeman:", testAppNamespace); err != nil {
			t.Fatalf("Error exercising test app endpoint after upgrade %s", err)
		}
	} else {
		t.Skip("Skipping for non upgrade test")
	}
}

func TestInstallSP(t *testing.T) {
	cmd := []string{"install-sp"}

	out, stderr, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("linkerd install-sp command failed\n%s\n%s", out, stderr)
	}

	out, err = TestHelper.KubectlApply(out, TestHelper.GetLinkerdNamespace())
	if err != nil {
		t.Fatalf("kubectl apply command failed\n%s", out)
	}
}

func TestDashboard(t *testing.T) {
	dashboardPort := 52237
	dashboardURL := fmt.Sprintf("http://localhost:%d", dashboardPort)

	outputStream, err := TestHelper.LinkerdRunStream("dashboard", "-p",
		strconv.Itoa(dashboardPort), "--show", "url")
	if err != nil {
		t.Fatalf("Error running command:\n%s", err)
	}
	defer outputStream.Stop()

	outputLines, err := outputStream.ReadUntil(4, 1*time.Minute)
	if err != nil {
		t.Fatalf("Error running command:\n%s", err)
	}

	output := strings.Join(outputLines, "")
	if !strings.Contains(output, dashboardURL) {
		t.Fatalf("Dashboard command failed. Expected url [%s] not present", dashboardURL)
	}

	resp, err := TestHelper.HTTPGetURL(dashboardURL + "/api/version")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !strings.Contains(resp, TestHelper.GetVersion()) {
		t.Fatalf("Dashboard command failed. Expected response [%s] to contain version [%s]",
			resp, TestHelper.GetVersion())
	}
}

func TestInject(t *testing.T) {
	resources, err := testutil.ReadFile("testdata/smoke_test.yaml")
	if err != nil {
		t.Fatalf("failed to read smoke test file: %s", err)
	}

	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			var out string

			prefixedNs := TestHelper.GetTestNamespace(tc.ns)

			err := TestHelper.CreateDataPlaneNamespaceIfNotExists(prefixedNs, tc.annotations)
			if err != nil {
				t.Fatalf("failed to create %s namespace: %s", prefixedNs, err)
			}

			if tc.injectArgs != nil {
				cmd := []string{"inject"}
				cmd = append(cmd, tc.injectArgs...)
				cmd = append(cmd, "testdata/smoke_test.yaml")

				var injectReport string
				out, injectReport, err = TestHelper.LinkerdRun(cmd...)
				if err != nil {
					t.Fatalf("linkerd inject command failed: %s\n%s", err, out)
				}

				err = TestHelper.ValidateOutput(injectReport, "inject.report.golden")
				if err != nil {
					t.Fatalf("Received unexpected output\n%s", err.Error())
				}
			} else {
				out = resources
			}

			out, err = TestHelper.KubectlApply(out, prefixedNs)
			if err != nil {
				t.Fatalf("kubectl apply command failed\n%s", out)
			}

			for _, deploy := range []string{"smoke-test-terminus", "smoke-test-gateway"} {
				err = TestHelper.CheckPods(prefixedNs, deploy, 1)
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
			}

			url, err := TestHelper.URLFor(prefixedNs, "smoke-test-gateway", 8080)
			if err != nil {
				t.Fatalf("Failed to get URL: %s", err)
			}

			output, err := TestHelper.HTTPGetURL(url)
			if err != nil {
				t.Fatalf("Unexpected error: %v %s", err, output)
			}

			expectedStringInPayload := "\"payload\":\"BANANA\""
			if !strings.Contains(output, expectedStringInPayload) {
				t.Fatalf("Expected application response to contain string [%s], but it was [%s]",
					expectedStringInPayload, output)
			}
		})
	}
}

func TestServiceProfileDeploy(t *testing.T) {
	bbProto, err := TestHelper.HTTPGetURL("https://raw.githubusercontent.com/BuoyantIO/bb/master/api.proto")
	if err != nil {
		t.Fatalf("Unexpected error: %v %s", err, bbProto)
	}

	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			prefixedNs := TestHelper.GetTestNamespace(tc.ns)

			cmd := []string{"profile", "-n", prefixedNs, "--proto", "-", "smoke-test-terminus-svc"}
			bbSP, stderr, err := TestHelper.PipeToLinkerdRun(bbProto, cmd...)
			if err != nil {
				t.Fatalf("Unexpected error: %v %s", err, stderr)
			}

			out, err := TestHelper.KubectlApply(bbSP, prefixedNs)
			if err != nil {
				t.Fatalf("kubectl apply command failed: %s\n%s", err, out)
			}
		})
	}
}

func TestCheckProxy(t *testing.T) {
	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			prefixedNs := TestHelper.GetTestNamespace(tc.ns)
			testCheckCommand(t, "proxy", TestHelper.GetVersion(), prefixedNs, "")
		})
	}
}

func TestLogs(t *testing.T) {
	controllerRegex := regexp.MustCompile("level=(panic|fatal|error|warn)")
	proxyRegex := regexp.MustCompile(fmt.Sprintf("%s (ERR|WARN)", k8s.ProxyContainerName))
	clientGoRegex := regexp.MustCompile("client-go@")
	hasClientGoLogs := false

	for deploy, spec := range linkerdDeployReplicas {
		deploy := strings.TrimPrefix(deploy, "linkerd-")
		containers := append(spec.containers, k8s.ProxyContainerName)

		for _, container := range containers {
			container := container // pin
			name := fmt.Sprintf("%s/%s", deploy, container)

			proxy := false
			errRegex := controllerRegex
			knownErrorsRegex := knownControllerErrorsRegex
			if container == k8s.ProxyContainerName {
				proxy = true
				errRegex = proxyRegex
				knownErrorsRegex = knownProxyErrorsRegex
			}

			t.Run(name, func(t *testing.T) {
				outputStream, err := TestHelper.LinkerdRunStream(
					"logs", "--no-color",
					"--control-plane-component", deploy,
					"--container", container,
				)
				if err != nil {
					t.Errorf("Error running command:\n%s", err)
				}
				defer outputStream.Stop()
				// Ignore the error returned, since ReadUntil will return an error if it
				// does not return 10,000 after 2 seconds. We don't need 10,000 log lines.
				outputLines, _ := outputStream.ReadUntil(10000, 2*time.Second)
				if len(outputLines) == 0 {
					// Retry one time for 30 more seconds, in case the cluster is slow to
					// produce log lines.
					outputLines, _ = outputStream.ReadUntil(10000, 30*time.Second)
					if len(outputLines) == 0 {
						t.Errorf("No logs found for %s", name)
					}
				}

				for _, line := range outputLines {
					if errRegex.MatchString(line) {
						if knownErrorsRegex.MatchString(line) {
							// report all known logging errors in the output
							t.Logf("Found known error in %s log: %s", name, line)
						} else {
							if proxy {
								t.Logf("Found unexpected proxy error in %s log: %s", name, line)
							} else {
								t.Errorf("Found unexpected controller error in %s log: %s", name, line)
							}
						}
					}
					if clientGoRegex.MatchString((line)) {
						hasClientGoLogs = true
					}
				}
			})
		}
	}
	if !hasClientGoLogs {
		t.Errorf("Didn't find any client-go entries")
	}
}

func TestEvents(t *testing.T) {
	out, err := TestHelper.Kubectl("",
		"--namespace", TestHelper.GetLinkerdNamespace(),
		"get", "events", "-ojson",
	)
	if err != nil {
		t.Errorf("kubectl get events command failed with %s\n%s", err, out)
	}

	events, err := testutil.ParseEvents(out)
	if err != nil {
		t.Fatal(err)
	}

	var unknownEvents []string
	for _, e := range events {
		if e.Type == corev1.EventTypeNormal {
			continue
		}

		evtStr := fmt.Sprintf("Reason: [%s] Object: [%s] Message: [%s]", e.Reason, e.InvolvedObject.Name, e.Message)
		if knownEventWarningsRegex.MatchString(e.Message) {
			t.Logf("Found known warning event: %s", evtStr)
		} else {
			unknownEvents = append(unknownEvents, evtStr)
		}
	}

	if len(unknownEvents) > 0 {
		t.Errorf("Found unexpected warning events:\n%s", strings.Join(unknownEvents, "\n"))
	}
}

func TestRestarts(t *testing.T) {
	for deploy, spec := range linkerdDeployReplicas {
		if err := TestHelper.CheckPods(TestHelper.GetLinkerdNamespace(), deploy, spec.replicas); err != nil {
			t.Fatal(fmt.Errorf("Error validating pods [%s]:\n%s", deploy, err))
		}
	}
}
