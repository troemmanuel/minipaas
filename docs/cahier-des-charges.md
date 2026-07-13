# MiniPaaS — Cahier des charges

Plateforme interne de déploiement (PaaS en miniature) : usine logicielle auto-hébergée, déploiement continu en GitOps et observabilité complète, le tout sur Kubernetes.

---

## 1. Contexte et objectif

Construire une plateforme permettant à une équipe de développement de **livrer une application conteneurisée de bout en bout**, sans intervention manuelle sur le cluster, avec traçabilité, sécurité et supervision.

L'application métier est volontairement minimale : **elle n'est pas le sujet**. L'objet du projet est la **chaîne de production logicielle** qui l'entoure.

### Objectifs fonctionnels

| # | Objectif |
|---|---|
| F1 | Un `git push` déclenche automatiquement build, tests et publication d'une image |
| F2 | Aucun déploiement n'est effectué en poussant depuis la CI vers le cluster |
| F3 | L'état du cluster est entièrement décrit dans Git (source de vérité unique) |
| F4 | Un rollback applicatif est possible en moins d'une minute |
| F5 | Aucun secret n'est stocké en clair dans un dépôt |
| F6 | Métriques, logs et alertes sont disponibles sur l'ensemble de la plateforme |
| F7 | La configuration de l'usine logicielle est versionnée, pas cliquée |

### Objectifs non fonctionnels

- **Reproductibilité** : le cluster complet est reconstruit de zéro en une commande.
- **Sécurité** : pas de conteneur privilégié, pas de secret en clair, images scannées.
- **Isolation** : les agents de build sont éphémères, pas d'état partagé entre builds.
- **Coût** : intégralement exécutable en local (aucun cloud payant requis).

---

## 2. Architecture

### Vue d'ensemble

```
        MONOREPO
┌───────────────────────┐
│  app/                 │  push (chemin app/**)   ┌──────────────┐
│    code + Dockerfile  │────────────────────────▶│   Jenkins    │
│                       │                         │ (in-cluster) │
│  deploy/              │                         └──────┬───────┘
│    chart Helm         │                                │ build (Kaniko)
│    values-*.yaml      │                                │ scan  (Trivy)
│    argocd/            │                                │ push
│                       │                                ▼
│  platform/            │                         ┌────────────┐
│    terraform/         │                         │  Registry  │
│    ansible/           │                         │  (Harbor)  │
│    jenkins/           │                         └─────┬──────┘
└───────────┬───────────┘                               │
            │                                           │ détecte le
            │ watch (path: deploy/chart)                │ nouveau tag
            │                                           │
            │              ┌──────────────┐◀────────────┘
            └─────────────▶│    ArgoCD    │
                           │ + Image Upd. │
                           └──────┬───────┘
                                  │ apply / self-heal
                           ┌──────▼───────┐
                           │  Kubernetes  │
                           └──────────────┘
```

### Principe directeur : GitOps

**La CI ne déploie jamais.** Elle produit une image conteneur et s'arrête là. ArgoCD observe l'état déclaré et fait converger le cluster vers celui-ci.

Conséquences directes :

- Le cluster ne fait confiance à aucun système externe : aucun credential de cluster n'est stocké dans la CI.
- Un rollback est un `git revert`.
- Toute dérive manuelle (`kubectl edit`) est détectée et corrigée automatiquement.

### Contrainte propre au monorepo : rompre la boucle de rétroaction

Dans une chaîne GitOps classique, le pipeline conclut en committant le nouveau tag d'image dans le dépôt d'infrastructure. En monorepo, ce commit atterrit **dans le dépôt que la CI surveille** : il déclenche un nouveau build, qui commit à nouveau, et ainsi de suite. La boucle est infinie.

**Solution retenue : Argo CD Image Updater.** Le composant surveille directement le registre d'images et met à jour le tag lui-même. Le pipeline s'arrête après la publication de l'image.

Bénéfices :

- Aucun commit émis par la CI, donc aucune boucle possible.
- Jenkins n'a plus besoin d'un token d'écriture Git, ce qui réduit sa surface d'attaque.
- Séparation nette des responsabilités : la CI produit des artefacts, le CD gère l'état du cluster.

**Défense en profondeur — filtres de chemin.** Indépendamment du point ci-dessus, Jenkins ne déclenche un build que si le répertoire `app/**` a été modifié. Un changement portant uniquement sur `deploy/**` ou `platform/**` ne relance pas la construction de l'image. C'est une bonne pratique générale en monorepo, appliquée ici également comme garde-fou.

*Solutions alternatives écartées : marqueur `[skip ci]` dans le message de commit du job de mise à jour (fonctionnel mais fragile, la boucle est rompue par convention et non par conception).*

### Choix d'organisation : monorepo

Le projet est organisé en **un dépôt unique**, découpé en trois domaines :

| Domaine | Contenu |
|---|---|
| `app/` | Code applicatif, `Dockerfile` |
| `deploy/` | Chart Helm, valeurs par environnement, `Application` ArgoCD, secrets scellés |
| `platform/` | Terraform, playbooks Ansible, configuration JCasC de Jenkins |

**Justification.** La séparation app / infra en dépôts distincts répond avant tout à un besoin de cloisonnement des droits : un accès en écriture au dépôt d'infrastructure équivaut à un accès au cluster, et l'on ne souhaite pas que les équipes applicatives puissent modifier les manifests de production. Sur un projet à contributeur unique, cet argument disparaît. Le monorepo apporte alors un historique unifié, un clone unique et aucune synchronisation de versions entre dépôts.

Le modèle GitOps est préservé : il ne dépend pas du nombre de dépôts, mais du fait que Git demeure l'unique source de vérité de l'état du cluster.

ArgoCD supporte nativement ce découpage : le champ `path` de la ressource `Application` pointe sur `deploy/chart`, le reste du dépôt étant ignoré.

> **Migration vers le multi-repo.** Le découpage en dossiers reflète exactement la frontière de séparation. Extraire `deploy/` dans un dépôt dédié consiste à changer le `repoURL` de l'`Application` ArgoCD — le reste est inchangé.

---

## 3. Stack technique

| Couche | Outil | Rôle |
|---|---|---|
| Application | Spring Boot ou FastAPI | API REST + producteur de messages |
| Base de données | PostgreSQL | Persistance (StatefulSet) |
| Cache | Redis | Cache applicatif |
| Broker | RabbitMQ | File de messages + worker consommateur |
| Conteneurisation | Docker (multi-stage) | Images applicatives |
| Orchestration | Kubernetes (k3d / kind) | Runtime |
| Provisioning | Terraform | Cluster, namespaces, quotas, releases Helm |
| Configuration | Ansible | Préparation et durcissement des nœuds |
| Packaging | Helm | Chart applicatif paramétrable |
| SCM | GitLab | Dépôts, merge requests |
| Registry | Harbor | Registre d'images privé + scan |
| CI | Jenkins (in-cluster, JCasC) | Build, test, scan, publication |
| Build d'image | Kaniko | Build sans daemon Docker ni privilèges |
| Scan de sécurité | Trivy | Vulnérabilités des images |
| CD | ArgoCD | Réconciliation GitOps |
| Mise à jour d'image | Argo CD Image Updater | Détection des nouveaux tags au registre |
| Secrets | Sealed Secrets | Secrets chiffrés versionnables |
| Métriques | Prometheus | Collecte et stockage |
| Dashboards | Grafana | Visualisation |
| Alerting | Alertmanager | Notification sur seuils |
| Logs | Loki (ou stack ELK) | Centralisation des journaux |

---

## 4. Étapes de réalisation

### Étape 1 — Application et conteneurisation

**Livrables**

- API exposant un CRUD, un endpoint `/health` et un endpoint `/metrics` (format Prometheus).
- Un worker consommant une file RabbitMQ.
- `Dockerfile` multi-stage.

**Critères d'acceptation**

- [ ] Image finale < 200 Mo.
- [ ] Le conteneur s'exécute avec un utilisateur non-root.
- [ ] `docker compose up` démarre l'ensemble (API + Postgres + Redis + RabbitMQ).

---

### Étape 2 — Socle Kubernetes (manifests bruts)

**Livrables**

- `Deployment` (API, worker), `StatefulSet` (PostgreSQL), `Service`, `Ingress`.
- `ConfigMap` (configuration), `Secret` (identifiants), `PersistentVolumeClaim`.
- Sondes `liveness` / `readiness` / `startup`.
- `requests` et `limits` définis sur tous les conteneurs.

**Critères d'acceptation**

- [ ] L'application est jointe via l'Ingress.
- [ ] Les données PostgreSQL survivent à la suppression du pod.
- [ ] Un pod tué manuellement est recréé automatiquement.

> Cette étape est réalisée **en YAML brut, sans Helm**. Le templating vient ensuite ; la compréhension des primitives d'abord.

---

### Étape 3 — Packaging Helm

**Livrables**

- Chart maison : `Chart.yaml`, `values.yaml`, `templates/`, `_helpers.tpl`.
- Deux jeux de valeurs : `values-dev.yaml` et `values-prod.yaml` (réplicas et ressources différenciés).

**Critères d'acceptation**

- [ ] `helm template` produit exactement les manifests de l'étape 2.
- [ ] `helm upgrade` puis `helm rollback` fonctionnent et sont tracés dans `helm history`.
- [ ] Aucune valeur en dur dans les templates.

---

### Étape 4 — Usine logicielle (Jenkins)

**Livrables**

- Jenkins déployé dans le cluster via son chart Helm (PVC, Ingress, admin en Secret).
- Configuration intégralement en **JCasC** (Configuration as Code), versionnée.
- Plugin Kubernetes : les agents de build sont des **pods éphémères** créés à la demande.
- `Jenkinsfile` déclaratif, versionné dans `repo-app` :

```groovy
pipeline {
  agent { kubernetes { yaml podTemplate } }
  // Monorepo : ne construire que si le domaine applicatif a changé
  triggers { /* déclenchement filtré sur app/** */ }
  stages {
    stage('Lint')  { /* ... */ }
    stage('Test')  { /* tests unitaires + couverture */ }
    stage('Build') { /* Kaniko → image taguée par commit SHA */ }
    stage('Scan')  { /* Trivy, échec si CVE critique */ }
    stage('Push')  { /* publication vers Harbor */ }
  }
  post {
    always  { /* publication des rapports */ }
    failure { /* notification */ }
  }
}
```

> **Le pipeline s'arrête après la publication de l'image.** Il n'écrit jamais dans le dépôt : c'est Argo CD Image Updater qui détecte le nouveau tag dans le registre (étape 5). Ce choix rompt par conception la boucle de rétroaction du monorepo et évite d'accorder un droit d'écriture Git à Jenkins.

- Multibranch Pipeline : découverte automatique des branches et merge requests.
- Filtre de chemin : un commit ne touchant que `deploy/**` ou `platform/**` ne déclenche pas de build.
- Credentials (accès registre) dans le Credentials Store, jamais en dur.

**Critères d'acceptation**

- [ ] Aucun conteneur `privileged` n'est utilisé pour construire les images (Kaniko, pas Docker-in-Docker).
- [ ] Aucun agent statique : `kubectl get pods` pendant un build montre un pod agent temporaire.
- [ ] Le pipeline échoue si une CVE critique est détectée.
- [ ] Une réinstallation de Jenkins restaure la configuration à l'identique depuis le code.
- [ ] Jenkins ne dispose d'aucun droit d'écriture sur le dépôt Git.
- [ ] Un commit portant uniquement sur `deploy/**` ne déclenche aucun build.

> **Choix d'architecture — Kaniko vs Docker-in-Docker :** DinD nécessite un conteneur privilégié, ce qui rompt l'isolation du cluster (une évasion depuis le pod donne un accès root au nœud). Kaniko construit l'image en espace utilisateur, sans privilèges.

---

### Étape 5 — Déploiement continu (ArgoCD)

**Livrables**

- ArgoCD installé dans le cluster.
- Une ressource `Application` par environnement, pointant sur le dépôt avec `path: deploy/chart` et le fichier de valeurs correspondant à l'environnement.
- Politique de synchronisation automatique avec `prune` et `selfHeal`.
- **Argo CD Image Updater** configuré : surveillance du registre, stratégie de mise à jour et contrainte de version (ex. `latest` par date de publication, ou expression semver).

**Critères d'acceptation**

- [ ] La boucle complète est vérifiée : commit sur `app/**` → build → image publiée dans Harbor → Image Updater détecte le nouveau tag → ArgoCD synchronise → nouveaux pods en ligne.
- [ ] Aucun commit n'est produit par la CI ; la boucle de rétroaction du monorepo n'existe pas.
- [ ] `kubectl delete deployment <app>` : ArgoCD recrée la ressource automatiquement (self-healing).
- [ ] Une modification manuelle par `kubectl edit` est écrasée à la synchronisation suivante (correction de dérive).
- [ ] Un `git revert` sur `deploy/` restaure la version précédente de la configuration.

---

### Étape 6 — Gestion des secrets

**Livrables**

- Sealed Secrets déployé (contrôleur + clé).
- Les identifiants PostgreSQL et RabbitMQ chiffrés en `SealedSecret`, versionnés dans `repo-infra`.

**Critères d'acceptation**

- [ ] Aucun secret en clair dans aucun dépôt.
- [ ] Le `SealedSecret` committé est inexploitable en dehors du cluster cible.
- [ ] Le contrôleur déchiffre et produit le `Secret` Kubernetes correspondant.

---

### Étape 7 — Observabilité

**Livrables**

- `kube-prometheus-stack` (Prometheus + Grafana + Alertmanager) déployé via Helm.
- `ServiceMonitor` sur l'API : scraping de l'endpoint `/metrics`.
- Un dashboard Grafana applicatif : taux de requêtes, taux d'erreur, latence (p50/p95/p99), saturation CPU/mémoire.
- Loki (ou stack ELK) pour la centralisation des logs, avec corrélation depuis Grafana.

**Critères d'acceptation**

- [ ] Les métriques applicatives remontent dans Prometheus.
- [ ] Le dashboard couvre les quatre signaux dorés (latence, trafic, erreurs, saturation).
- [ ] Les logs de tous les pods sont interrogeables depuis une interface unique.

---

### Étape 8 — Infrastructure as Code

**Livrables**

- **Terraform** : cluster, namespaces, `ResourceQuota`, `LimitRange`, installation d'ArgoCD et de la stack de supervision via le provider Helm. Backend distant avec verrouillage d'état.
- **Ansible** : playbook de préparation d'un nœud (paquets, Docker, k3s, utilisateurs, durcissement SSH), organisé en rôles.

**Critères d'acceptation**

- [ ] `terraform apply` reconstruit la plateforme complète depuis zéro.
- [ ] `terraform plan` sur une infrastructure inchangée ne propose aucune modification (idempotence).
- [ ] Le playbook Ansible est idempotent : une seconde exécution ne signale aucun changement.

> **Répartition des responsabilités :** Terraform *provisionne* (crée et détruit des ressources, gère leur cycle de vie via un state). Ansible *configure* (amène l'état interne d'une machine existante vers l'état désiré).

---

### Étape 9 — Fiabilité (SRE)

**Livrables**

- Un **SLI** défini : latence des requêtes HTTP.
- Un **SLO** : 99 % des requêtes servies en moins de 300 ms sur une fenêtre glissante de 30 jours.
- **Error budget** correspondant, et une règle d'alerte Prometheus sur son taux de consommation.
- Un test de résilience : suppression répétée de pods pendant une injection de charge.

**Critères d'acceptation**

- [ ] L'alerte se déclenche et remonte dans Alertmanager lorsque le seuil est franchi.
- [ ] Le service reste disponible pendant le test de résilience (aucune erreur 5xx côté client).
- [ ] Un `runbook` décrit la marche à suivre pour chaque alerte définie.

---

## 5. Arborescence cible

```
minipaas/
├── app/                          # Domaine applicatif
│   ├── api/
│   │   ├── src/
│   │   └── Dockerfile
│   ├── worker/
│   │   ├── src/
│   │   └── Dockerfile
│   └── docker-compose.yml        # Exécution locale sans Kubernetes
│
├── deploy/                       # Domaine déploiement (observé par ArgoCD)
│   ├── chart/
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   ├── _helpers.tpl
│   │   └── templates/
│   ├── environments/
│   │   ├── values-dev.yaml
│   │   └── values-prod.yaml
│   ├── argocd/
│   │   └── application.yaml      # path: deploy/chart
│   └── secrets/
│       └── postgres-sealed.yaml
│
├── platform/                     # Domaine plateforme
│   ├── terraform/
│   │   ├── main.tf
│   │   ├── variables.tf
│   │   └── modules/
│   ├── ansible/
│   │   ├── inventory/
│   │   ├── roles/
│   │   └── site.yml
│   └── jenkins/
│       └── jcasc.yaml            # Configuration as Code
│
├── docs/
│   ├── architecture.md
│   └── runbooks/
│
├── Jenkinsfile                   # Déclenché sur app/** uniquement
└── README.md
```

---

## 6. Démarrage rapide

```bash
# 1. Cluster local
k3d cluster create minipaas --agents 2 -p "80:80@loadbalancer"

# 2. Plateforme : ArgoCD, Image Updater, Jenkins, supervision
cd platform/terraform && terraform init && terraform apply

# 3. ArgoCD prend le relais : il synchronise deploy/chart et déploie l'application
kubectl get applications -n argocd

# 4. Exécution locale sans Kubernetes (développement)
cd app && docker compose up
```

---

## 7. Points de vigilance

- **Ne pas court-circuiter l'étape 2.** Le templating Helm masque les primitives Kubernetes ; il faut les avoir écrites à la main au moins une fois.
- **Ne jamais donner les credentials du cluster à la CI.** C'est précisément ce que le modèle GitOps élimine.
- **Jenkins n'écrit pas dans le dépôt.** En monorepo, tout commit émis par la CI dans le dépôt qu'elle surveille crée une boucle de build infinie. La mise à jour du tag d'image est déléguée à Argo CD Image Updater.
- **Aucun `kubectl apply` manuel** une fois ArgoCD en place : toute modification passe par Git, sinon la dérive sera écrasée à la prochaine synchronisation.
- **Ressources et sondes obligatoires** sur chaque conteneur : sans `requests`, le scheduler ne peut pas faire son travail ; sans sondes, Kubernetes ne peut pas savoir si l'application est réellement saine.
- **Le découpage en dossiers est une frontière, pas une convention cosmétique.** `app/`, `deploy/` et `platform/` ont des cycles de vie, des déclencheurs et des droits distincts. Les mélanger reviendrait à perdre le bénéfice du monorepo sans en éviter les inconvénients.

---

## 8. Extensions possibles

- Déploiement progressif (canary / blue-green) avec Argo Rollouts.
- Politiques d'admission avec Kyverno ou OPA Gatekeeper.
- Traçage distribué (OpenTelemetry + Tempo/Jaeger) pour compléter métriques et logs.
- Authentification centralisée (Keycloak) devant les interfaces de la plateforme.
- Autoscaling horizontal basé sur des métriques applicatives (KEDA).
- Migration du cluster local vers un cluster managé (AKS, EKS) via Terraform.
