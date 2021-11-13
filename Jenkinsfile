pipeline {
    
    agent none

    environment {
        COMPOSE_PROJECT_NAME = "${env.JOB_NAME}-${env.BUILD_ID}"
    }
    stages {
      
        stage('Build') {

            agent any

            steps {
                
                echo sh(script: 'env|sort', returnStdout: true)

                sh  '''

                    docker build . -f ./Jornada/Dockerfile -t registry.oragon.io/jornada_gago_io/jornada-webapp:${BRANCH_NAME:-master}
               
                '''

            }

        }

        stage('Publish') {

            agent any

            when { buildingTag() }


            steps {
                
                sh  '''

                    docker tag registry.oragon.io/jornada_gago_io/jornada-webapp:${BRANCH_NAME:-master} registry.oragon.io/jornada_gago_io/jornada-webapp:latest

                    docker push registry.oragon.io/jornada_gago_io/jornada-webapp:${BRANCH_NAME:-master} 
                    
                    docker push registry.oragon.io/jornada_gago_io/jornada-webapp:latest
               
                '''

                withCredentials(bindings: [sshUserPrivateKey(credentialsId: 'SERVER_ADMIN', \
                                             keyFileVariable: 'SERVER_ADMIN_KEY', \
                                             passphraseVariable: 'SERVER_ADMIN_PWD', \
                                             usernameVariable: 'SERVER_ADMIN_USER')]) {
                    sh  '''
                    
                    ssh -p 7777 -i $SERVER_ADMIN_KEY $SERVER_ADMIN_USER@oragon02.oragon.io docker pull registry.oragon.io/jornada_gago_io/jornada-webapp:${BRANCH_NAME:-master}

                    ssh -p 7777 -i $SERVER_ADMIN_KEY $SERVER_ADMIN_USER@oragon02.oragon.io docker service update --image registry.oragon.io/jornada_gago_io/jornada-webapp:${BRANCH_NAME:-master}  --env-add NODE_ID="{{.Node.ID}}" --env-add APP_VERSION=${BRANCH_NAME:-master} jornada_jornada_gago_io

                    '''
                }                

            }

        }


 
    }
    post {

        always {
            node('master'){
                
                sh  '''
               
                '''
            }
        }
    }
}