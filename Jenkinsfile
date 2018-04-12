#!/usr/bin/env groovy
def helmiImage
def versionNumber

pipeline {
    agent {
        node {
            label 'docker'
        }
    }
    options {
        buildDiscarder(logRotator(numToKeepStr: '15'))
        skipDefaultCheckout()
        skipStagesAfterUnstable()
        timeout(time: 10, unit: 'MINUTES')
        timestamps()
    }
    stages {
        stage('Checkout') {
            steps {
                gitCheckout()
                script {
                    versionNumber = VersionNumber(skipFailedBuilds: true, versionNumberString: '${BUILD_DATE_FORMATTED, \"yy\"}.${BUILD_WEEK}.${BUILDS_THIS_WEEK}')
                    gitCommit = gitCommit()
                    shortCommit = gitCommit.take(7)
                    gitCommitMessage = sh returnStdout: true, script: 'git log -1 --pretty=%B HEAD | xargs echo -n'
                    currentBuild.displayName = versionNumber
                    if (gitCommitMessage) {
                        currentBuild.description = gitCommitMessage
                    } else {
                        currentBuild.description = BRANCH_NAME + ' / ' + shortCommit
                    }
                }
                sh "echo 'Branch=${BRANCH_NAME}' >> version.properties"
                sh "echo 'SHA1=${gitCommit}' >> version.properties"
                sh "echo 'Version=${versionNumber}' >> version.properties"
            }
        }
        stage('Build') {
            steps {
                retry(2) {
                    script {
                        helmiImage = docker.build('helmi', "--label 'com.monostream.image.branch=${BRANCH_NAME}' --label 'com.monostream.image.sha1=${gitCommit}' --label 'com.monostream.image.version=${versionNumber}' --no-cache --pull --squash .")
                    }
                }
            }
        }
        stage('Unit Test') {
            steps {
                script {
                    docker.image('golang:alpine').inside("-v '${WORKSPACE}:/go/src/github.com/monostream/helmi/' -u root") {
                        sh 'apk add --no-cache --update git'
                        sh '''
                        cd /go/src/github.com/monostream/helmi/
                        go get github.com/jstemmer/go-junit-report
                        go test -v ./pkg/* | go-junit-report > report.xml
                        '''
                    }
                }
                junit 'report.xml'
            }
        }
        stage('Push to ECR') {
            when {
                expression { BRANCH_NAME == 'master' || BRANCH_NAME == 'develop' }
            }
            steps {
                retry(2) {
                    script {
                        docker.image('monostream/helmi') {
                            parallel(
                                    "${BRANCH_NAME}": {
                                        if (BRANCH_NAME == 'develop') {
                                            helmiImage.push("${versionNumber}-dev")
                                        }
                                        if (BRANCH_NAME == 'master') {
                                            helmiImage.push("${versionNumber}")
                                        }
                                    },
                                    'latest': {
                                        helmiImage.push()
                                    },
                                    failFast: true
                            )
                        }
                    }
                }
            }
        }
        stage('Artefact') {
            steps {
                script {
                    def Artefacts = ['catalog.yaml', 'helm', 'helmi']
                    helmiImage.inside {
                        for (int i = 0; i < Artefacts.size(); ++i) {
                            sh "cp -rf /app/${Artefacts[i]} ${WORKSPACE}"
                            archive "${Artefacts[i]}"
                        }
                    }
                    archive "version.properties"
                }
            }
        }
    }
    post {
        always {
            withChownWorkspace()
            deleteDir()
        }
    }
}
