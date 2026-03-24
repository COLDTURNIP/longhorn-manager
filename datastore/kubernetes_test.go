package datastore

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	longhorn "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	lhfake "github.com/longhorn/longhorn-manager/k8s/pkg/client/clientset/versioned/fake"
	lhinformerfactory "github.com/longhorn/longhorn-manager/k8s/pkg/client/informers/externalversions"
	"github.com/longhorn/longhorn-manager/types"
)

func TestNewPVCManifestForVolume(t *testing.T) {
	tests := map[string]struct {
		volume             *longhorn.Volume
		expectedAccessMode corev1.PersistentVolumeAccessMode
	}{
		"read write once": {
			volume: &longhorn.Volume{
				Spec: longhorn.VolumeSpec{
					Size:       1024 * 1024 * 1024, // 1Gi
					AccessMode: longhorn.AccessModeReadWriteOnce,
				},
			},
			expectedAccessMode: corev1.ReadWriteOnce,
		},
		"read write many": {
			volume: &longhorn.Volume{
				Spec: longhorn.VolumeSpec{
					Size:       1024 * 1024 * 1024, // 1Gi
					AccessMode: longhorn.AccessModeReadWriteMany,
				},
			},
			expectedAccessMode: corev1.ReadWriteMany,
		},
		"read write once pod": {
			volume: &longhorn.Volume{
				Spec: longhorn.VolumeSpec{
					Size:       1024 * 1024 * 1024, // 1Gi
					AccessMode: longhorn.AccessModeReadWriteOncePod,
				},
			},
			expectedAccessMode: corev1.ReadWriteOncePod,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			pvc := NewPVCManifestForVolume(tc.volume, "pv-name", "default", "pvc-name", "longhorn")
			require.NotNil(t, pvc)
			assert.Equal(t, []corev1.PersistentVolumeAccessMode{tc.expectedAccessMode}, pvc.Spec.AccessModes)
		})
	}
}

func TestNewPVManifestForVolumeAttributesAndAccessModes(t *testing.T) {
	newVolume := func(mode longhorn.AccessMode, migratable, encrypted bool, replicas, srt int, diskSel, nodeSel []string) *longhorn.Volume {
		return &longhorn.Volume{
			Spec: longhorn.VolumeSpec{
				Size:                2 * 1024 * 1024 * 1024, // 2Gi
				AccessMode:          mode,
				Migratable:          migratable,
				Encrypted:           encrypted,
				NumberOfReplicas:    replicas,
				StaleReplicaTimeout: srt,
				DiskSelector:        diskSel,
				NodeSelector:        nodeSel,
			},
		}
	}

	t.Run("rwop volume manifest attributes", func(t *testing.T) {
		v := newVolume(longhorn.AccessModeReadWriteOncePod, false, true, 3, 2880, []string{"ssd"}, []string{"fast"})
		pv := NewPVManifestForVolume(v, "pv-rwop", "longhorn", "ext4")
		require.NotNil(t, pv)
		assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOncePod}, pv.Spec.AccessModes)
		attrs := pv.Spec.CSI.VolumeAttributes
		require.NotNil(t, attrs)
		assert.Equal(t, "ssd", attrs["diskSelector"])
		assert.Equal(t, "fast", attrs["nodeSelector"])
		assert.Equal(t, "3", attrs["numberOfReplicas"])
		assert.Equal(t, "2880", attrs["staleReplicaTimeout"])
		assert.Equal(t, "true", attrs["encrypted"])
		_, hasMigratable := attrs["migratable"]
		assert.False(t, hasMigratable)
	})

	t.Run("rwx volume manifest attributes", func(t *testing.T) {
		v := newVolume(longhorn.AccessModeReadWriteMany, true, false, 2, 1440, []string{"nvme", "hot"}, []string{"zone-a"})
		pv := NewPVManifestForVolume(v, "pv-rwx", "longhorn", "ext4")
		require.NotNil(t, pv)
		assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}, pv.Spec.AccessModes)
		attrs := pv.Spec.CSI.VolumeAttributes
		require.NotNil(t, attrs)
		assert.Equal(t, "nvme,hot", attrs["diskSelector"])
		assert.Equal(t, "zone-a", attrs["nodeSelector"])
		assert.Equal(t, "2", attrs["numberOfReplicas"])
		assert.Equal(t, "1440", attrs["staleReplicaTimeout"])
		assert.Equal(t, "true", attrs["migratable"])
		_, hasEncrypted := attrs["encrypted"]
		assert.False(t, hasEncrypted)
	})
}

const testNamespace = "longhorn-system"

func newTestDataStoreWithSettings(t *testing.T, objects ...runtime.Object) (*DataStore, func()) {
	t.Helper()
	lhClient := lhfake.NewSimpleClientset(objects...) // nolint: staticcheck
	informerFactory := lhinformerfactory.NewSharedInformerFactory(lhClient, 0)
	settingInformer := informerFactory.Longhorn().V1beta2().Settings()

	ds := &DataStore{
		namespace:       testNamespace,
		lhClient:        lhClient,
		settingLister:   settingInformer.Lister(),
		SettingInformer: settingInformer.Informer(),
	}

	stopCh := make(chan struct{})
	go ds.SettingInformer.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, ds.SettingInformer.HasSynced) {
		t.Fatal("failed to sync setting informer cache")
	}

	return ds, func() { close(stopCh) }
}

func newSettingCR(name types.SettingName, value string) *longhorn.Setting {
	return &longhorn.Setting{
		ObjectMeta: metav1.ObjectMeta{
			Name:      string(name),
			Namespace: testNamespace,
		},
		Value: value,
	}
}

func TestGetDataEngineIPFamily(t *testing.T) {
	tests := map[string]struct {
		settingValue string
		hasSetting   bool
		expected     string
	}{
		"ipv6 setting": {
			settingValue: fmt.Sprintf("{%q:\"ipv6\"}", longhorn.DataEngineTypeV2),
			hasSetting:   true,
			expected:     "ipv6",
		},
		"ipv4 setting": {
			settingValue: fmt.Sprintf("{%q:\"ipv4\"}", longhorn.DataEngineTypeV2),
			hasSetting:   true,
			expected:     "ipv4",
		},
		"empty setting value": {
			settingValue: "",
			hasSetting:   true,
			expected:     "ipv4",
		},
		"no setting CR": {
			hasSetting: false,
			expected:   "ipv4",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var objects []runtime.Object
			if tc.hasSetting {
				objects = append(objects, newSettingCR(types.SettingNameDataEngineIPFamily, tc.settingValue))
			}
			ds, cleanup := newTestDataStoreWithSettings(t, objects...)
			defer cleanup()

			result := ds.getDataEngineIPFamily()
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetIPFromPodByCNISettingIPv6(t *testing.T) {
	dualStackPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-im-pod",
			Namespace: testNamespace,
		},
		Status: corev1.PodStatus{
			PodIP: "10.0.0.1",
			PodIPs: []corev1.PodIP{
				{IP: "10.0.0.1"},
				{IP: "fd00::1"},
			},
		},
	}

	ipv6OnlyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-im-pod",
			Namespace: testNamespace,
		},
		Status: corev1.PodStatus{
			PodIP: "fd00::1",
			PodIPs: []corev1.PodIP{
				{IP: "fd00::1"},
			},
		},
	}

	cniDualStackPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-im-pod",
			Namespace: testNamespace,
			Annotations: map[string]string{
				string(types.CNIAnnotationNetworkStatus): `[{"name":"kube-system/my-storage-net","ips":["10.0.0.5","fd00::5"]}]`,
			},
		},
		Status: corev1.PodStatus{
			PodIP: "10.0.0.1",
			PodIPs: []corev1.PodIP{
				{IP: "10.0.0.1"},
				{IP: "fd00::1"},
			},
		},
	}

	tests := map[string]struct {
		pod            *corev1.Pod
		ipFamilyValue  string
		storageNetwork string
		expected       string
	}{
		"dual-stack pod, ipv4 setting (default)": {
			pod:            dualStackPod,
			ipFamilyValue:  fmt.Sprintf("{%q:\"ipv4\"}", longhorn.DataEngineTypeV2),
			storageNetwork: "",
			expected:       "10.0.0.1",
		},
		"dual-stack pod, ipv6 setting": {
			pod:            dualStackPod,
			ipFamilyValue:  fmt.Sprintf("{%q:\"ipv6\"}", longhorn.DataEngineTypeV2),
			storageNetwork: "",
			expected:       "fd00::1",
		},
		"ipv6-only pod, ipv4 setting": {
			pod:            ipv6OnlyPod,
			ipFamilyValue:  fmt.Sprintf("{%q:\"ipv4\"}", longhorn.DataEngineTypeV2),
			storageNetwork: "",
			expected:       "fd00::1",
		},
		"CNI dual-stack, ipv6 setting": {
			pod:            cniDualStackPod,
			ipFamilyValue:  fmt.Sprintf("{%q:\"ipv6\"}", longhorn.DataEngineTypeV2),
			storageNetwork: "kube-system/my-storage-net",
			expected:       "fd00::5",
		},
		"CNI dual-stack, ipv4 setting (regression)": {
			pod:            cniDualStackPod,
			ipFamilyValue:  fmt.Sprintf("{%q:\"ipv4\"}", longhorn.DataEngineTypeV2),
			storageNetwork: "kube-system/my-storage-net",
			expected:       "10.0.0.5",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			objects := []runtime.Object{
				newSettingCR(types.SettingNameDataEngineIPFamily, tc.ipFamilyValue),
				newSettingCR(types.SettingNameStorageNetwork, tc.storageNetwork),
			}
			ds, cleanup := newTestDataStoreWithSettings(t, objects...)
			defer cleanup()

			result := ds.GetIPFromPodByCNISetting(tc.pod, types.SettingNameStorageNetwork, true)
			assert.Equal(t, tc.expected, result)
		})
	}
}
