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

                    docker build . -f ./echo-server/Dockerfile -t registry.oragon.io/tools/echo:${BRANCH_NAME:-master}
               
                '''

            }

        }

        stage('Publish') {

            agent any

            when { buildingTag() }


            steps {
                
                sh  '''

                    docker tag registry.oragon.io/tools/echo:${BRANCH_NAME:-master} registry.oragon.io/tools/echo:latest
               
                '''

                withCredentials([usernamePassword(credentialsId: 'registry.oragon.io', passwordVariable: 'REGISTRY_PASSWORD', usernameVariable: 'REGISTRY_USER' )])
                {
                    sh  '''

                        echo $REGISTRY_PASSWORD | docker login -u $REGISTRY_USER --password-stdin registry.oragon.io 

                        docker push registry.oragon.io/tools/echo:${BRANCH_NAME:-master} 

                        docker push registry.oragon.io/tools/echo:latest

                    '''
                }
            }

        }


 
    }
    /*
    post {

        always {
            node('master'){
                
                sh  '''
               
                '''
            }
        }
    }
    */
}