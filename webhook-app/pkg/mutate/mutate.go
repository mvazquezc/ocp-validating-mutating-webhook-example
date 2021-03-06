// Package mutate deals with AdmissionReview requests and responses, it takes in the request body and returns a readily converted JSON []byte that can be
// returned from a http Handler w/o needing to further convert or modify it, it also makes testing Mutate() kind of easy w/o need for a fake http server, etc.
package mutate

import (
	"encoding/json"
	"fmt"
	"log"

	v1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Mutate mutates
func Mutate(body []byte, verbose bool) ([]byte, error) {
	if verbose {
		log.Printf("recv: %s\n", string(body))
	}

	// unmarshal request into AdmissionReview struct
	admReview := v1beta1.AdmissionReview{}
	if err := json.Unmarshal(body, &admReview); err != nil {
		return nil, fmt.Errorf("unmarshaling request failed with %s", err)
	}

	var err error
	var pod *corev1.Pod

	responseBody := []byte{}
	ar := admReview.Request
	resp := v1beta1.AdmissionResponse{}

	if ar != nil {

		// get the Pod object and unmarshal it into its struct, if we cannot, we might as well stop here
		if err := json.Unmarshal(ar.Object.Raw, &pod); err != nil {
			return nil, fmt.Errorf("unable unmarshal pod json object %v", err)
		}
		// set response options
		resp.Allowed = true
		resp.UID = ar.UID
		pT := v1beta1.PatchTypeJSONPatch
		resp.PatchType = &pT // it's annoying that this needs to be a pointer as you cannot give a pointer to a constant?

		// add some audit annotations, helpful to know why a object was modified, maybe (?)
		resp.AuditAnnotations = map[string]string{
			"removedResourceRequestsAndLimits": "true",
		}

		// the actual mutation is done by a string in JSONPatch style, i.e. we don't _actually_ modify the object, but
		// tell K8S how it should modifiy it
		p := []map[string]string{}

		for i := range pod.Spec.Containers {
			// Only remove resources for non-guaranteed containers
			containerName := pod.Spec.Containers[i].Name
			containerCpuRequests := pod.Spec.Containers[i].Resources.Requests.Cpu().Value()
			containerCpuLimits := pod.Spec.Containers[i].Resources.Limits.Cpu().Value()
			containerMemoryRequests := pod.Spec.Containers[i].Resources.Requests.Memory().Value()
			containerMemoryLimits := pod.Spec.Containers[i].Resources.Limits.Memory().Value()
			log.Printf("Container %s, Requests: [CPU: %d, Memory: %d], Limits: [CPU: %d, Memory: %d]", containerName, containerCpuRequests, containerMemoryRequests, containerCpuLimits, containerMemoryLimits)
			if ((containerCpuRequests + containerCpuLimits + containerMemoryRequests + containerMemoryLimits) == 0 ) {
				log.Print("Container is in the BestEffort QoS. Skipping...")
				continue
			} else if ((containerCpuRequests == containerCpuLimits) && (containerMemoryRequests == containerMemoryLimits)) {
				log.Print("Container is in the Guaranteed QoS. Skipping...")
				continue
			} else {
				log.Print("Container is in the Burstable QoS. Removing resources requests and limits from container.")
			}

			patch := map[string]string{
				"op":    "remove",
				"path":  fmt.Sprintf("/spec/containers/%d/resources", i),
			}
			p = append(p, patch)
		}
		// parse the []map into JSON
		resp.Patch, err = json.Marshal(p)

		// Success
		resp.Result = &metav1.Status{
			Status: "Success",
		}

		admReview.Response = &resp
		// back into JSON so we can return the finished AdmissionReview w/ Response directly
		// w/o needing to convert things in the http handler
		responseBody, err = json.Marshal(admReview)
		if err != nil {
			return nil, err // untested section
		}
	}

	if verbose {
		log.Printf("resp: %s\n", string(responseBody))
	}

	return responseBody, nil
}
