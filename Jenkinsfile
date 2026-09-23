// xiaoan-platform CI/CD — one pipeline builds both images and rolls out K8s.
// Flow: GitLab(test branch) → Node 22 frontend build → 2 docker images →
// Harbor(eboat2) → set image on 6 Deployments → rollout check.
//
// BEFORE first run, adjust:
//   GIT_CREDENTIAL_ID (inline below) — Jenkins credential with GitLab read access
//   Harbor login — company Docker login mechanism on the Jenkins host
//   Namespace/Ingress/Secret/ConfigMap — one-time kubectl applies (see k8s/README.md)
//
// This pipeline NEVER runs database migrations (manual ops step) and never
// creates Namespace/Secret/ConfigMap — those are one-time kubectl applies.

pipeline {
    agent any

    // SKIP_DEPLOY=true: build & push images only, do not touch K8s.
    // Used on FIRST run (workloads not applied yet) and for image-only rebuilds.
    parameters {
        booleanParam(name: 'SKIP_DEPLOY', defaultValue: false, description: '只构建并推送镜像，不更新 K8s')
    }

    environment {
        HARBOR          = 'harbor.hengan.com:8086'
        HARBOR_PROJECT  = 'eboat2'
        NAMESPACE       = 'eboat-ai-ns'
        APP_BASE_PATH   = '/xiaoan-platform/'
        GIT_CREDENTIAL_ID = 'xiaoan-gitlab-http'
    }

    stages {

        stage('下载代码') {
            steps {
                git branch: 'test',
                    credentialsId: "${GIT_CREDENTIAL_ID}",
                    url: 'http://192.168.0.81/ai_sys/xiaoan-platform.git'
            }
        }

        stage('准备版本号') {
            steps {
                script {
                    env.GIT_SHORT_SHA = sh(
                        script: 'git rev-parse --short=8 HEAD',
                        returnStdout: true
                    ).trim()

                    env.IMAGE_TAG = "test-${new Date().format('yyyyMMddHHmmss')}-${env.GIT_SHORT_SHA}"
                }
                echo "IMAGE_TAG=${env.IMAGE_TAG}"
            }
        }

        stage('编译前端') {
            steps {
                sh '''
                    source /etc/profile

                    # npm ci for reproducible installs
                    nodedkbuild "$WORKSPACE/frontend" \
                      "node22140" \
                      "npm ci"

                    # Build with K8s upstream service names baked into nginx.generated.conf.
                    # prebuild hook downloads OCR models (6.1 MB, sha256-pinned); after the
                    # first successful build the cache makes this step fully offline.
                    nodedkbuild "$WORKSPACE/frontend" \
                      "node22140" \
                      "APP_BASE_PATH=/xiaoan-platform/ NGINX_API_UPSTREAM=xiaoan-api:8080 NGINX_STREAM_UPSTREAM=xiaoan-stream:8081 npm run build"

                    if [ ! -d "$WORKSPACE/frontend/dist" ]; then
                        echo "前端编译失败"
                        exit 1
                    fi

                    if [ ! -f "$WORKSPACE/frontend/nginx.generated.conf" ]; then
                        echo "nginx.generated.conf 未生成，部署脚本未执行"
                        exit 1
                    fi
                '''
            }
        }

        stage('构建前端镜像') {
            steps {
                sh '''
                    docker build \
                      -f frontend/Dockerfile \
                      -t ${HARBOR}/${HARBOR_PROJECT}/xiaoan-ui:${IMAGE_TAG} \
                      frontend
                '''
            }
        }

        stage('构建后端镜像') {
            steps {
                sh '''
                    docker build \
                      -f backend-go/Dockerfile \
                      -t ${HARBOR}/${HARBOR_PROJECT}/xiaoan-backend:${IMAGE_TAG} \
                      backend-go
                '''
            }
        }

        stage('推送镜像') {
            steps {
                sh '''
                    docker push ${HARBOR}/${HARBOR_PROJECT}/xiaoan-ui:${IMAGE_TAG}

                    docker push ${HARBOR}/${HARBOR_PROJECT}/xiaoan-backend:${IMAGE_TAG}
                '''
            }
        }

        stage('更新 K8s') {
            when { expression { return !params.SKIP_DEPLOY } }
            steps {
                sh '''
                    kubectl -n ${NAMESPACE} set image \
                      deployment/xiaoan-ui \
                      xiaoan-ui=${HARBOR}/${HARBOR_PROJECT}/xiaoan-ui:${IMAGE_TAG}

                    kubectl -n ${NAMESPACE} set image \
                      deployment/xiaoan-api \
                      xiaoan-api=${HARBOR}/${HARBOR_PROJECT}/xiaoan-backend:${IMAGE_TAG}

                    kubectl -n ${NAMESPACE} set image \
                      deployment/xiaoan-stream \
                      xiaoan-stream=${HARBOR}/${HARBOR_PROJECT}/xiaoan-backend:${IMAGE_TAG}

                    kubectl -n ${NAMESPACE} set image \
                      deployment/xiaoan-worker-aily \
                      xiaoan-worker-aily=${HARBOR}/${HARBOR_PROJECT}/xiaoan-backend:${IMAGE_TAG}

                    kubectl -n ${NAMESPACE} set image \
                      deployment/xiaoan-worker-delivery \
                      xiaoan-worker-delivery=${HARBOR}/${HARBOR_PROJECT}/xiaoan-backend:${IMAGE_TAG}

                    kubectl -n ${NAMESPACE} set image \
                      deployment/xiaoan-scheduler \
                      xiaoan-scheduler=${HARBOR}/${HARBOR_PROJECT}/xiaoan-backend:${IMAGE_TAG}
                '''
            }
        }

        stage('检查发布状态') {
            when { expression { return !params.SKIP_DEPLOY } }
            steps {
                sh '''
                    set -e

                    kubectl rollout status deployment/xiaoan-ui -n ${NAMESPACE} --timeout=180s
                    kubectl rollout status deployment/xiaoan-api -n ${NAMESPACE} --timeout=180s
                    kubectl rollout status deployment/xiaoan-stream -n ${NAMESPACE} --timeout=180s
                    kubectl rollout status deployment/xiaoan-worker-aily -n ${NAMESPACE} --timeout=180s
                    kubectl rollout status deployment/xiaoan-worker-delivery -n ${NAMESPACE} --timeout=180s
                    kubectl rollout status deployment/xiaoan-scheduler -n ${NAMESPACE} --timeout=180s
                '''
            }
        }
    }
}
