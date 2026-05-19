package alertmanager

import "github.com/prometheus/alertmanager/api/v2/models"

// Alert is a thin wrapper around the AlertManager API's GettableAlert type.
type Alert = models.GettableAlert

// LabelSet is a set of key-value pairs used for alert labels and annotations.
type LabelSet = models.LabelSet
