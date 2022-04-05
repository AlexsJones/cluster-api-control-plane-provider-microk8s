package controllers

import "time"

const (
	// deleteRequeueAfter is how long to wait before checking again to see if
	// all control plane machines have been deleted.
	deleteRequeueAfter = 30 * time.Second

	// preflightFailedRequeueAfter is how long to wait before trying to scale
	// up/down if some preflight check for those operation has failed.
	preflightFailedRequeueAfter = 15 * time.Second

	// dependentCertRequeueAfter is how long to wait before checking again to see if
	// dependent certificates have been created.
	dependentCertRequeueAfter = 30 * time.Second
)
