package handlers

import (
	"context"
	"fmt"
	"os"

	"github.com/ViaQ/logerr/kverrors"
	"github.com/ViaQ/logerr/log"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/external/k8s"
	"github.com/ViaQ/loki-operator/internal/manifests"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateOrUpdateLokiStack handles LokiStack create and update events.
func CreateOrUpdateLokiStack(ctx context.Context, req ctrl.Request, k k8s.Client, s *runtime.Scheme) error {
	ll := log.WithValues("lokistack", req.NamespacedName, "event", "createOrUpdate")

	var stack lokiv1beta1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &stack); err != nil {
		if apierrors.IsNotFound(err) {
			// maybe the user deleted it before we could react? Either way this isn't an issue
			ll.Error(err, "could not find the requested loki stack", "name", req.NamespacedName)
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup lokistack", "name", req.NamespacedName)
	}

	img := os.Getenv("RELATED_IMAGE_LOKI")
	if img == "" {
		img = manifests.DefaultContainerImage
	}

	opts := manifests.Options{
		Name:      req.Name,
		Namespace: req.Namespace,
		Image:     img,
		Stack:     stack.Spec,
	}

	ll.Info("begin building manifests")

	objects, err := manifests.BuildAll(opts)
	if err != nil {
		ll.Error(err, "failed to build manifests")
		return err
	}
	ll.Info("manifests built", "count", len(objects))

	var errCount int32
	for _, obj := range objects {
		l := ll.WithValues(
			"object_name", obj.GetName(),
			"object_kind", obj.GetObjectKind(),
		)

		obj.SetNamespace(req.Namespace)

		if err := ctrl.SetControllerReference(&stack, obj, s); err != nil {
			l.Error(err, "failed to set controller owner reference to resource")
			errCount++
			continue
		}

		desired := obj.DeepCopyObject().(client.Object)
		mutateFn := manifests.MutateFuncFor(obj, desired)

		op, err := ctrl.CreateOrUpdate(ctx, k, obj, mutateFn)
		if err != nil {
			l.Error(err, "failed to configure resource")
			errCount++
			continue
		}
		l.Info(fmt.Sprintf("Resource has been %s", op))
	}

	if errCount > 0 {
		return kverrors.New("failed to configure lokistack resources", "name", req.NamespacedName)
	}

	return nil
}
