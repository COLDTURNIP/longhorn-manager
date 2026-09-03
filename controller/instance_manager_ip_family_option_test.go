package controller

import (
	"testing"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	apiextensionsfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8scontroller "k8s.io/kubernetes/pkg/controller"

	"github.com/longhorn/longhorn-manager/datastore"
	longhorn "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	lhfake "github.com/longhorn/longhorn-manager/k8s/pkg/client/clientset/versioned/fake"
	"github.com/longhorn/longhorn-manager/types"
	"github.com/longhorn/longhorn-manager/util"
)

func TestInstanceManagerIPFamilyOptionAfterDaemon(t *testing.T) {
	base := []string{"instance-manager", "--debug", "daemon"}

	require.Equal(t, base, appendInstanceManagerIPFamilyArgs(append([]string{}, base...), types.DataEngineIPFamilyDefault))
	for _, family := range []string{types.DataEngineIPFamilyIPv4, types.DataEngineIPFamilyIPv6} {
		args := appendInstanceManagerIPFamilyArgs(append([]string{}, base...), family)
		require.Equal(t, append(base, "--ip-family", family), args)
		require.Equal(t, "daemon", args[2])
		require.Equal(t, []string{"--ip-family", family}, args[3:])
	}
}

func TestInstanceManagerIPFamilyOptionRejectsInvalidValues(t *testing.T) {
	for _, family := range []string{"IPv4", "IPv6", "default", "", "ipv4x"} {
		got, specified, valid := types.ParseDataEngineIPFamilyArgs([]string{"daemon", "--ip-family", family})
		require.True(t, specified, "family %q should be recognized as specified", family)
		require.False(t, valid, "family %q should be rejected", family)
		require.Empty(t, got)

		args := appendInstanceManagerIPFamilyArgs([]string{"daemon"}, family)
		require.Equal(t, []string{"daemon"}, args)
	}
}

func newInstanceManagerIPFamilyOptionController(t *testing.T, family string) *InstanceManagerController {
	t.Helper()
	kubeClient := kubefake.NewSimpleClientset()
	lhClient := lhfake.NewSimpleClientset()
	extensionsClient := apiextensionsfake.NewSimpleClientset()
	informerFactories := util.NewInformerFactories(TestNamespace, kubeClient, lhClient, k8scontroller.NoResyncPeriodFunc())
	ds := datastore.NewDataStore(TestNamespace, lhClient, kubeClient, extensionsClient, informerFactories)
	require.NoError(t, ds.SettingInformer.GetStore().Add(newSetting(string(types.SettingNamePreferredDataEngineIPFamily), family)))
	require.NoError(t, ds.SettingInformer.GetStore().Add(newSetting(string(types.SettingNameV1DataEngine), "true")))
	return &InstanceManagerController{namespace: TestNamespace, ds: ds}
}

func newInstanceManagerIPFamilyOptionPod(name string, family string) *corev1.Pod {
	args := []string{"instance-manager", "--debug", "daemon"}
	args = appendInstanceManagerIPFamilyArgs(args, family)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: TestNamespace},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "instance-manager", Args: args}}},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			PodIP:             "2001:db8::10",
			ContainerStatuses: []corev1.ContainerStatus{{Name: "instance-manager", Ready: true}},
		},
	}
}

func TestSettingDataEngineIPFamilySyncedMatchesExactPodOption(t *testing.T) {
	imc := newInstanceManagerIPFamilyOptionController(t, types.DataEngineIPFamilyIPv6)
	setting := newSetting(string(types.SettingNamePreferredDataEngineIPFamily), types.DataEngineIPFamilyIPv6)
	pod := newInstanceManagerIPFamilyOptionPod(TestInstanceManagerName, types.DataEngineIPFamilyIPv6)

	synced, err := imc.isSettingDataEngineIPFamilySynced(setting, pod)
	require.NoError(t, err)
	require.True(t, synced)
}

func TestSettingDataEngineIPFamilySyncedRejectsMismatchedPodOption(t *testing.T) {
	imc := newInstanceManagerIPFamilyOptionController(t, types.DataEngineIPFamilyIPv6)
	setting := newSetting(string(types.SettingNamePreferredDataEngineIPFamily), types.DataEngineIPFamilyIPv6)
	pod := newInstanceManagerIPFamilyOptionPod(TestInstanceManagerName, types.DataEngineIPFamilyIPv4)

	synced, err := imc.isSettingDataEngineIPFamilySynced(setting, pod)
	require.NoError(t, err)
	require.False(t, synced)
}

func TestSyncInstanceManagerIPFamilyUsesRunningReadyPodOption(t *testing.T) {
	imc := newInstanceManagerIPFamilyOptionController(t, types.DataEngineIPFamilyIPv6)
	pod := newInstanceManagerIPFamilyOptionPod(TestInstanceManagerName, types.DataEngineIPFamilyIPv6)
	require.NoError(t, imc.ds.PodInformer.GetStore().Add(pod))

	oldFamily := types.DataEngineIPFamilyIPv4
	im := &longhorn.InstanceManager{
		ObjectMeta: metav1.ObjectMeta{Name: TestInstanceManagerName, Namespace: TestNamespace},
		Spec: longhorn.InstanceManagerSpec{
			DataEngine: longhorn.DataEngineTypeV1,
		},
		Status: longhorn.InstanceManagerStatus{
			CurrentState: longhorn.InstanceManagerStateRunning,
			IPFamily:     &oldFamily,
		},
	}

	blocked, err := imc.syncInstanceManagerIPFamily(im)
	require.NoError(t, err)
	require.False(t, blocked)
	require.NotNil(t, im.Status.IPFamily)
	require.Equal(t, types.DataEngineIPFamilyIPv6, *im.Status.IPFamily)
	require.Equal(t, "2001:db8::10", im.Status.IP)
}

func TestSyncInstanceManagerIPFamilyRejectsStorageNetworkFamilyFailure(t *testing.T) {
	imc := newInstanceManagerIPFamilyOptionController(t, types.DataEngineIPFamilyIPv6)
	require.NoError(t, imc.ds.SettingInformer.GetStore().Add(newSetting(string(types.SettingNameStorageNetwork), "storage-net")))
	pod := newInstanceManagerIPFamilyOptionPod(TestInstanceManagerName, types.DataEngineIPFamilyIPv6)
	pod.Annotations = map[string]string{
		string(types.CNIAnnotationNetworkStatus): `[{"name":"storage-net","interface":"lhnet1","ips":["192.0.2.10"]}]`,
	}
	require.NoError(t, imc.ds.PodInformer.GetStore().Add(pod))

	oldFamily := types.DataEngineIPFamilyIPv4
	im := &longhorn.InstanceManager{
		ObjectMeta: metav1.ObjectMeta{Name: TestInstanceManagerName, Namespace: TestNamespace},
		Spec: longhorn.InstanceManagerSpec{
			DataEngine: longhorn.DataEngineTypeV1,
		},
		Status: longhorn.InstanceManagerStatus{
			CurrentState: longhorn.InstanceManagerStateRunning,
			IPFamily:     &oldFamily,
		},
	}

	blocked, err := imc.syncInstanceManagerIPFamily(im)
	require.NoError(t, err)
	require.True(t, blocked)
	require.Equal(t, "", im.Status.IP)
	require.NotNil(t, im.Status.IPFamily)
	require.Equal(t, oldFamily, *im.Status.IPFamily)
	require.Equal(t, longhorn.ConditionStatusFalse,
		types.GetCondition(im.Status.Conditions, longhorn.InstanceManagerConditionTypeSettingSynced).Status)
}
func TestSettingDataEngineIPFamilySyncedSignalsAttachedVolumeBlock(t *testing.T) {
	imc := newInstanceManagerIPFamilyOptionController(t, types.DataEngineIPFamilyIPv6)
	require.NoError(t, imc.ds.VolumeInformer.GetStore().Add(&longhorn.Volume{
		ObjectMeta: metav1.ObjectMeta{Name: "attached-volume", Namespace: TestNamespace},
		Status:     longhorn.VolumeStatus{State: longhorn.VolumeStateAttached},
	}))
	setting := newSetting(string(types.SettingNamePreferredDataEngineIPFamily), types.DataEngineIPFamilyIPv6)
	pod := newInstanceManagerIPFamilyOptionPod(TestInstanceManagerName, types.DataEngineIPFamilyIPv4)

	synced, err := imc.isSettingDataEngineIPFamilySynced(setting, pod)
	require.ErrorIs(t, err, errDataEngineIPFamilyBlockedByAttachedVolumes)
	require.False(t, synced)
}
