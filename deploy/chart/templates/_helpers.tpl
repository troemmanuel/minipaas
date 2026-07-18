{{/*
Nom complet des ressources : "<release>-<chart>", tronqué à 63 caractères
(limite imposée par Kubernetes sur les noms de la plupart des objets).
*/}}
{{- define "minipaas.fullname" -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Labels communs, posés sur TOUTES les ressources (convention officielle
Kubernetes app.kubernetes.io/*). Contient des valeurs versionnées
(version du chart/de l'appli) -- ne JAMAIS réutiliser pour un selector,
voir minipaas.selectorLabels ci-dessous.
*/}}
{{- define "minipaas.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{/*
Labels de sélection : sous-ensemble STABLE utilisé dans matchLabels/selector.
Kubernetes interdit de modifier le selector d'un Deployment/StatefulSet
existant -- ces labels ne doivent donc JAMAIS contenir de valeur qui change
d'une release à l'autre (pas de version ici, contrairement à labels ci-dessus).
*/}}
{{- define "minipaas.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}