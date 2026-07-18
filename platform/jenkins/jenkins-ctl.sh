#!/usr/bin/env bash
#
# jenkins-ctl.sh -- CLI de pilotage Jenkins depuis le terminal : voir les
# builds en cours/en attente/bloqués, les relancer, les arrêter, les
# supprimer -- sans passer par l'UI web (volontairement lourde à naviguer).
#
# --------------------------------------------------------------------------
# MISE EN PLACE (une seule fois)
# --------------------------------------------------------------------------
#   cp platform/jenkins/.env.example platform/jenkins/.env
#   # puis édite platform/jenkins/.env avec tes vrais identifiants Jenkins
#   chmod +x platform/jenkins/jenkins-ctl.sh
#
# Prérequis : jq installé (`brew install jq`), et Jenkins joignable via
# l'Ingress (curl -H "Host: jenkins.minipaas.local" http://localhost doit
# répondre -- pas besoin de port-forward).
#
# --------------------------------------------------------------------------
# COMMANDES (à lancer depuis n'importe où, ou via ./jenkins-ctl.sh depuis
# platform/jenkins/)
# --------------------------------------------------------------------------
#
#   status
#     Vue d'ensemble : file d'attente (builds bloqués/en attente de
#     ressources), dernier statut connu de chaque branche indexée, et les
#     pods agents éphémères actuellement actifs sur le cluster.
#     Exemple : ./jenkins-ctl.sh status
#
#   restart <branche>
#     Déclenche un nouveau build sur la branche donnée (équivalent du
#     bouton "Build Now" de l'UI web).
#     Exemple : ./jenkins-ctl.sh restart main
#
#   stop <branche> <numero>
#     Arrête un build en cours d'exécution. Le numéro de build s'obtient
#     via `status` (colonne "#N").
#     Exemple : ./jenkins-ctl.sh stop main 12
#
#   delete <branche> <numero>
#     Supprime définitivement un build de l'historique Jenkins (logs,
#     artefacts...). Irréversible.
#     Exemple : ./jenkins-ctl.sh delete main 7
#
#   cancel <queue-id>
#     Annule un item bloqué dans la file d'attente AVANT qu'il ne démarre
#     (différent de `stop`, qui agit sur un build déjà en cours). L'ID
#     s'obtient via `status`, section "File d'attente".
#     Exemple : ./jenkins-ctl.sh cancel 42
#
#   logs <branche> <numero>
#     Affiche la sortie console complète d'un build (équivalent de la page
#     "Console Output" de l'UI web).
#     Exemple : ./jenkins-ctl.sh logs main 12 | less
#
#   scan
#     Force un rescan immédiat des branches/PR du dépôt GitHub (au lieu
#     d'attendre le periodicFolderTrigger de 5 min défini dans jcasc.yaml).
#     Utile après un premier push pour ne pas attendre.
#     Exemple : ./jenkins-ctl.sh scan
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Charge .env s'il existe (jamais committé, voir .env.example pour le format)
if [[ -f "$SCRIPT_DIR/.env" ]]; then
    set -a
    source "$SCRIPT_DIR/.env"
    set +a
fi

JENKINS_URL="${JENKINS_URL:-http://localhost}"
JENKINS_HOST_HEADER="${JENKINS_HOST_HEADER:-jenkins.minipaas.local}"
JENKINS_USER="${JENKINS_USER:?JENKINS_USER manquant (voir .env.example)}"
JENKINS_PASSWORD="${JENKINS_PASSWORD:?JENKINS_PASSWORD manquant (voir .env.example)}"
JENKINS_FOLDER="${JENKINS_FOLDER:-minipaas}"

COOKIE_JAR="$(mktemp)"
trap 'rm -f "$COOKIE_JAR"' EXIT

RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; BLUE=$'\033[34m'; BOLD=$'\033[1m'; RESET=$'\033[0m'

# curl vers l'API Jenkins, avec l'auth + le header Host + le crumb CSRF déjà réglés.
# $1 = méthode HTTP, $2 = chemin (ex: /api/json), le reste = options curl additionnelles.
jenkins_api() {
    local method="$1" path="$2"; shift 2
    local crumb
    crumb=$(curl -s -c "$COOKIE_JAR" -u "$JENKINS_USER:$JENKINS_PASSWORD" \
        -H "Host: $JENKINS_HOST_HEADER" "$JENKINS_URL/crumbIssuer/api/json" \
        | jq -r '.crumb')
    curl -s -b "$COOKIE_JAR" -u "$JENKINS_USER:$JENKINS_PASSWORD" \
        -H "Host: $JENKINS_HOST_HEADER" -H "Jenkins-Crumb: $crumb" \
        -X "$method" "$JENKINS_URL$path" "$@"
}

job_path() {
    # Convertit un nom de branche en chemin d'API Jenkins : main -> job/minipaas/job/main
    echo "job/$JENKINS_FOLDER/job/$1"
}

status_icon() {
    case "$1" in
        SUCCESS) echo "${GREEN}✓ SUCCESS${RESET}" ;;
        FAILURE) echo "${RED}✗ FAILURE${RESET}" ;;
        ABORTED) echo "${YELLOW}⊘ ABORTED${RESET}" ;;
        null|"") echo "${BLUE}● BUILDING${RESET}" ;;
        *) echo "$1" ;;
    esac
}

cmd_status() {
    echo "${BOLD}== File d'attente (bloqués/en attente) ==${RESET}"
    local queue
    queue=$(jenkins_api GET "/queue/api/json")
    local queue_count
    queue_count=$(echo "$queue" | jq '.items | length')
    if [[ "$queue_count" -eq 0 ]]; then
        echo "  (vide)"
    else
        echo "$queue" | jq -r '.items[] | "  [\(.id)] \(.task.name) -- \(.why // "en attente")"'
    fi

    echo
    echo "${BOLD}== Branches et derniers builds ==${RESET}"
    local branches
    branches=$(jenkins_api GET "/job/$JENKINS_FOLDER/api/json" | jq -r '.jobs[].name')
    if [[ -z "$branches" ]]; then
        echo "  Aucune branche indexée. Lance './jenkins-ctl.sh scan' pour forcer un scan."
        return
    fi
    while IFS= read -r branch; do
        local info number building result
        info=$(jenkins_api GET "/$(job_path "$branch")/lastBuild/api/json" 2>/dev/null || echo '{}')
        number=$(echo "$info" | jq -r '.number // "-"')
        building=$(echo "$info" | jq -r '.building // false')
        result=$(echo "$info" | jq -r '.result')
        if [[ "$building" == "true" ]]; then
            echo "  ${BOLD}$branch${RESET} #$number -- $(status_icon "")"
        else
            echo "  ${BOLD}$branch${RESET} #$number -- $(status_icon "$result")"
        fi
    done <<< "$branches"

    echo
    echo "${BOLD}== Pods agents éphémères actuellement actifs ==${RESET}"
    kubectl get pods -n cicd -l jenkins=slave 2>/dev/null | grep -v "^NAME" || echo "  (aucun agent en cours)"
}

cmd_restart() {
    local branch="${1:?Usage: restart <branche>}"
    jenkins_api POST "/$(job_path "$branch")/build" -o /dev/null -w "HTTP %{http_code}\n"
    echo "Build déclenché pour '$branch'."
}

cmd_stop() {
    local branch="${1:?Usage: stop <branche> <numero>}" number="${2:?Usage: stop <branche> <numero>}"
    jenkins_api POST "/$(job_path "$branch")/$number/stop" -o /dev/null -w "HTTP %{http_code}\n"
    echo "Build #$number de '$branch' arrêté."
}

cmd_delete() {
    local branch="${1:?Usage: delete <branche> <numero>}" number="${2:?Usage: delete <branche> <numero>}"
    jenkins_api POST "/$(job_path "$branch")/$number/doDelete" -o /dev/null -w "HTTP %{http_code}\n"
    echo "Build #$number de '$branch' supprimé."
}

cmd_cancel() {
    local queue_id="${1:?Usage: cancel <queue-id>}"
    jenkins_api POST "/queue/cancelItem?id=$queue_id" -o /dev/null -w "HTTP %{http_code}\n"
    echo "Item de file d'attente #$queue_id annulé."
}

cmd_logs() {
    local branch="${1:?Usage: logs <branche> <numero>}" number="${2:?Usage: logs <branche> <numero>}"
    jenkins_api GET "/$(job_path "$branch")/$number/consoleText"
}

cmd_scan() {
    jenkins_api POST "/job/$JENKINS_FOLDER/build" -o /dev/null -w "HTTP %{http_code}\n"
    echo "Rescan des branches déclenché."
}

case "${1:-status}" in
    status)  cmd_status ;;
    restart) shift; cmd_restart "$@" ;;
    stop)    shift; cmd_stop "$@" ;;
    delete)  shift; cmd_delete "$@" ;;
    cancel)  shift; cmd_cancel "$@" ;;
    logs)    shift; cmd_logs "$@" ;;
    scan)    cmd_scan ;;
    *)
        echo "Commande inconnue: $1" >&2
        echo "Usage: $0 {status|restart|stop|delete|cancel|logs|scan} [args]" >&2
        exit 1
        ;;
esac
