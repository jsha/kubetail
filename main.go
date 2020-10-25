// kubetail tails the logs from all pods as of a certain amount of time ago,
// and prints them to stdout.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func main() {
	nameFilter := flag.String("name-filter", "", "substring filter for pod names to get logs from")
	namespace := flag.String("namespace", "", "namespace to query for pods")
	since := flag.String("since", "10m", "An amount of time in the past to look for logs. Go-style Duration")
	flag.Parse()

	sinceDur, err := time.ParseDuration(*since)
	if err != nil {
		log.Fatal(err)
	}

	if err := main2(*nameFilter, *namespace, sinceDur); err != nil {
		log.Fatal(err)
	}
}

func main2(nameFilter string, namespace string, sinceDur time.Duration) error {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		return fmt.Errorf("building k8s client config: %w", err)
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("building clientset: %w", err)
	}

	corev1 := clientset.CoreV1()

	pods, err := corev1.Pods(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("getting pods: %w", err)
	}
	var printed int
	for _, pod := range pods.Items {
		name := pod.Name
		if !strings.Contains(name, nameFilter) {
			continue
		}
		sinceTime := metav1.NewTime(time.Now().Add(-sinceDur))
		podLogs, err := corev1.Pods(pod.Namespace).GetLogs(name, &v1.PodLogOptions{
			SinceTime: &sinceTime,
		}).Stream(context.Background())
		if err != nil {
			log.Printf("getting logs for %q: %s", name, err)
			continue
		}

		// Read one byte to detect empty logs before we bother printing their names.
		var onebyte [1]byte
		n, err := podLogs.Read(onebyte[:])
		if err == io.EOF || n == 0 {
			continue
		}
		if printed > 0 {
			// Add a newline between files
			fmt.Println()
		}
		fmt.Printf("==> %s <==\n", name)
		_, err = os.Stdout.Write(onebyte[:])
		if err != nil {
			return fmt.Errorf("writing one byte: %w", err)
		}

		_, err = io.Copy(os.Stdout, podLogs)
		if err != nil {
			log.Printf("streaming logs for %q: %s", name, err)
			podLogs.Close()
			continue
		}
		podLogs.Close()
	}
	return nil
}
