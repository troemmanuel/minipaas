# MiniPaaS

Plateforme interne de déploiement (PaaS en miniature) : usine logicielle auto-hébergée, déploiement continu en GitOps et observabilité complète, le tout sur Kubernetes.

L'application métier livrée par la plateforme est volontairement minimale : l'objet du projet est la **chaîne de production logicielle** qui l'entoure (build, sécurité, déploiement, supervision), pas l'application elle-même.

## Principes clés

- **GitOps** : Git est l'unique source de vérité de l'état du cluster. La CI ne déploie jamais ; ArgoCD observe et fait converger le cluster.
- **Monorepo à trois domaines** : `apps/` (code applicatif), `deploy/` (Helm, ArgoCD, secrets scellés), `platform/` (Terraform, Ansible, Jenkins).
- **Rupture de la boucle de rétroaction** : Argo CD Image Updater détecte les nouveaux tags d'image au registre, sans que la CI n'ait besoin d'écrire dans le dépôt.
- **Sécurité par conception** : pas de conteneur privilégié (Kaniko plutôt que Docker-in-Docker), pas de secret en clair (Sealed Secrets), images scannées (Trivy).
- **Reproductibilité** : l'ensemble de la plateforme se reconstruit depuis zéro via Terraform et Ansible.

## Stack technique

Spring Boot/FastAPI, PostgreSQL, Redis, RabbitMQ, Docker, Kubernetes (k3d/kind), Terraform, Ansible, Helm, GitLab, Harbor, Jenkins (JCasC), Kaniko, Trivy, ArgoCD + Image Updater, Sealed Secrets, Prometheus, Grafana, Alertmanager, Loki.

## Structure du dépôt

```
minipaas/
├── apps/         # Code applicatif (apps/crud-go : API + worker + Dockerfiles)
├── deploy/       # Chart Helm, valeurs par environnement, ArgoCD, secrets scellés
├── platform/     # Terraform, Ansible, configuration Jenkins (JCasC)
└── docs/         # Documentation, cahier des charges, runbooks
```

## Documentation

Le détail complet du projet — contexte, architecture, choix techniques justifiés, étapes de réalisation avec critères d'acceptation, et points de vigilance — se trouve dans le [cahier des charges](docs/cahier-des-charges.md).

## Démarrage rapide

```bash
# 1. Cluster local
k3d cluster create minipaas --agents 2 -p "80:80@loadbalancer"

# 2. Plateforme : ArgoCD, Image Updater, Jenkins, supervision
cd platform/terraform && terraform init && terraform apply

# 3. ArgoCD prend le relais : il synchronise deploy/chart et déploie l'application
kubectl get applications -n argocd

# 4. Exécution locale sans Kubernetes (développement)
cd apps/crud-go && docker compose up
```

## Application (apps/crud-go)

API CRUD en Go (`api/`) + worker consommateur RabbitMQ (`worker/`), avec PostgreSQL, Redis et RabbitMQ. Voir [Étape 1 du cahier des charges](docs/cahier-des-charges.md#étape-1--application-et-conteneurisation).

- `GET /health` — état des dépendances (Postgres, Redis, RabbitMQ)
- `GET /metrics` — métriques au format Prometheus
- `GET/POST /items`, `GET/PUT/DELETE /items/{id}` — CRUD, avec cache Redis en lecture et publication d'événements RabbitMQ (`item.created`/`item.updated`/`item.deleted`) consommés par le worker

```bash
cd apps/crud-go
docker compose up -d
curl http://localhost:8080/health
curl -X POST http://localhost:8080/items -H "Content-Type: application/json" -d '{"name":"demo"}'
```
