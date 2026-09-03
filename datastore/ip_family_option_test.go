package datastore

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/longhorn/longhorn-manager/types"
)

func TestPrimaryDataEngineIPPrefersFirstLHNet1Address(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				string(types.CNIAnnotationNetworkStatus): `[{"name":"pod-net","interface":"eth0","ips":["10.0.0.2"]},{"name":"storage-net","interface":"lhnet1","ips":["2001:db8::20","2001:db8::21"]}]`,
			},
		},
		Status: corev1.PodStatus{PodIP: "10.0.0.2"},
	}
	require.Equal(t, "2001:db8::20", getPrimaryDataEngineIP(pod))

	pod.Annotations = nil
	require.Equal(t, "10.0.0.2", getPrimaryDataEngineIP(pod))
}

func TestDataEngineIPFamilyRequiresUsableAddress(t *testing.T) {
	require.True(t, isDataEngineIPFamily("192.0.2.10", types.DataEngineIPFamilyIPv4))
	require.True(t, isDataEngineIPFamily("2001:db8::10", types.DataEngineIPFamilyIPv6))
	require.False(t, isDataEngineIPFamily("127.0.0.1", types.DataEngineIPFamilyIPv4))
	require.False(t, isDataEngineIPFamily("fe80::1", types.DataEngineIPFamilyIPv6))
	require.False(t, isDataEngineIPFamily("2001:db8::10", types.DataEngineIPFamilyIPv4))
}
