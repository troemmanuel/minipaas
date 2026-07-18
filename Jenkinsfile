// Pipeline déclaratif -- s'exécute dans un pod éphémère (label "kaniko",
// voir platform/jenkins/jcasc.yaml), détruit à la fin du build, qu'il
// réussisse ou échoue. Aucun agent statique.
pipeline {
    agent {
        kubernetes {
            cloud "kaniko-cloud"
            label "kaniko"
        }
    }

    // Pas de vrai webhook GitHub possible depuis un cluster local (pas d'URL
    // publique) -- on vérifie donc périodiquement s'il y a du nouveau.
    triggers {
        pollSCM('H/5 * * * *')
    }

    environment {
        REGISTRY     = "registry.cicd.svc.cluster.local:5000"
        IMAGE_API    = "${REGISTRY}/crud-go-api"
        IMAGE_WORKER = "${REGISTRY}/crud-go-worker"
        // Tag = SHA court du commit -- traçabilité directe entre une image
        // et le code exact qui l'a produite (contrairement à "latest").
        TAG          = "${env.GIT_COMMIT.take(7)}"
    }

    stages {
        stage('Lint') {
            // Sans plugin de filtrage au niveau du déclenchement, on ne peut
            // pas empêcher le build de démarrer -- mais on peut garantir
            // qu'aucun travail réel ne s'exécute si seul deploy/**
            // ou platform/** a changé.
            when {
                changeset "apps/**"
            }
            steps {
                container('golang') {
                    dir('apps/crud-go') {
                        sh 'go vet ./...'
                    }
                }
            }
        }

        stage('Test') {
            when {
                changeset "apps/**"
            }
            steps {
                container('golang') {
                    dir('apps/crud-go') {
                        sh 'go test ./... -cover'
                    }
                }
            }
        }

        stage('Build') {
            when {
                changeset "apps/**"
            }
            steps {
                container('kaniko') {
                    dir('apps/crud-go') {
                        // Kaniko construit puis pousse en une seule commande
                        // -- pas d'étape "docker push" séparée, il n'y a pas
                        // de démon Docker pour retenir l'image entre les deux.
                        //
                        // Les deux appels sont dans UN SEUL bloc sh (donc un
                        // seul "kubectl exec" dans le conteneur) : l'exécutable
                        // Kaniko n'est pas conçu pour être invoqué une seconde
                        // fois via un nouvel exec dans le même conteneur --
                        // ça échoue avec "Process exited immediately after
                        // creation" si on sépare en deux étapes sh distinctes.
                        sh """
                            /kaniko/executor \
                              --context=`pwd` \
                              --dockerfile=`pwd`/api/Dockerfile \
                              --destination=${IMAGE_API}:${TAG} \
                              --insecure --skip-tls-verify

                            /kaniko/executor \
                              --context=`pwd` \
                              --dockerfile=`pwd`/worker/Dockerfile \
                              --destination=${IMAGE_WORKER}:${TAG} \
                              --insecure --skip-tls-verify
                        """
                    }
                }
            }
        }

        stage('Scan') {
            when {
                changeset "apps/**"
            }
            steps {
                container('trivy') {
                    // --exit-code 1 fait échouer l'étape (donc le pipeline)
                    // si une CVE CRITICAL est détectée -- c'est le critère
                    // d'acceptation, pas une option cosmétique.
                    sh "trivy image --insecure --exit-code 1 --severity CRITICAL ${IMAGE_API}:${TAG}"
                    sh "trivy image --insecure --exit-code 1 --severity CRITICAL ${IMAGE_WORKER}:${TAG}"
                }
            }
        }
    }

    post {
        always {
            echo "Pipeline termine pour le commit ${env.GIT_COMMIT}"
        }
        failure {
            echo "Echec du pipeline -- voir les logs de l'etape en erreur ci-dessus"
        }
    }
}
