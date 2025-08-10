Scaffold is from the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

# K8s Scheduled Scale Operator

## Overview

K8s Scheduled Scale Operator is a Kubernetes custom controller built with Kubebuilder.
It enables automatic scaling of Deployments based on a specified time window and restores the original replica count once the window ends.

The primary goal is to reduce resource costs and handle periodic workload changes (e.g., shut down services at night, scale up during working hours).

⸻

## Features
	•	Scheduled scaling — Configure start and end times for automatic scaling
	•	Replica preservation & restoration — Save the original replica count in annotations before scaling down, and restore it afterward
	•	Continuous loop execution — The controller constantly monitors and performs scaling actions
	•	Status tracking — CR status transitions through Pending → Scaled → Restored

⸻

## Workflow
Process summary:

	1. 	Enter loop and check the CR status
	2.	If status="" and conditions match → store the original replica count in annotations
	3.	Within the time window, if not yet scaled → execute scaleDeployment
	4.	After the time window, if status="Scaled" → execute restoreDeployment
	5.	Update status to Restored and repeat
