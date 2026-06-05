// cienergy - Jenkins shared library step.
//
// Place this file under `vars/cienergyMeasure.groovy` in a Jenkins Shared
// Library, then in a Jenkinsfile:
//
//   @Library('cienergy') _
//
//   pipeline {
//     agent { label 'linux' }
//     stages {
//       stage('Build') {
//         steps {
//           cienergyMeasure(region: 'WE', team: 'claims-platform',
//                           costCenter: 'AXA-FR-IT-042',
//                           ingesterUrl: 'https://cienergy.example.com',
//                           ingesterTokenCred: 'cienergy-token') {
//             sh './build.sh'
//             sh './test.sh'
//           }
//         }
//       }
//     }
//   }
//
// Required Jenkins plugins: Pipeline Utility Steps, Credentials Binding.
// The cienergy-aggregator binary is downloaded from the GitHub release page
// (override via `aggregatorUrl:`).

def call(Map config = [:], Closure body) {
    Map cfg = [
        region:            'WORLD',
        cpuModel:          'Intel Xeon Platinum 8370C',
        cpuTdpW:           270,
        team:              '',
        costCenter:        '',
        ingesterUrl:       '',
        ingesterTokenCred: '',     // Jenkins credentials ID of a SecretText
        electricityMapsTokenCred: '',
        aggregatorUrl:     'https://github.com/axa-oss/cienergytool/releases/latest/download/cienergy-aggregator-linux-x86_64',
        artifactName:      'cienergy-report',
    ] << config

    String work = "${env.WORKSPACE}/.cienergy"
    String startVar = 'CIENERGY_START_TS'

    // ----- Pre-job: install + start sampler --------------------------------
    sh """
        set -euo pipefail
        mkdir -p '${work}'
        if [ ! -x '${work}/cienergy-aggregator' ]; then
            curl -fsSL -o '${work}/cienergy-aggregator' '${cfg.aggregatorUrl}'
            chmod +x '${work}/cienergy-aggregator'
        fi
    """
    env."${startVar}" = sh(script: 'date -u +%FT%TZ', returnStdout: true).trim()
    String pid = sh(returnStdout: true, script: """
        nohup bash -c '
          while true; do
            if command -v top >/dev/null; then
              U=\$(top -bn1 2>/dev/null | awk "/Cpu/ {print 100 - \\\$8; exit}")
            else
              U=50
            fi
            echo "{\\\"t\\\":\\\"\$(date -u +%FT%TZ)\\\",\\\"util\\\":\${U:-50}}" >> '${work}/util.jsonl'
            sleep 2
          done
        ' >/dev/null 2>&1 &
        echo \$!
    """).trim()

    int exitCode = 0
    try {
        body()
    } catch (err) {
        exitCode = 1
        throw err
    } finally {
        // ----- Post-job: aggregate ----------------------------------------
        withEnv([
            "CIENERGY_TEAM=${cfg.team}",
            "CIENERGY_COST_CENTER=${cfg.costCenter}",
            "CIENERGY_WORK=${work}",
            "CIENERGY_PID=${pid}",
        ]) {
            // Optional Electricity Maps token
            def aggregateClosure = {
                sh """
                    set -euo pipefail
                    kill \$CIENERGY_PID 2>/dev/null || true
                    AVG_UTIL=\$(awk -F'\"util\":' 'NF>1 {gsub(/[},]/,\"\",\$2); s+=\$2; n++} END{if(n>0) print s/n; else print 50}' '${work}/util.jsonl' 2>/dev/null || echo 50)
                    START=\$${startVar}
                    DURATION=\$(( \$(date -u +%s) - \$(date -u -d \"\$START\" +%s 2>/dev/null || gdate -u -d \"\$START\" +%s) ))
                    printf '{\"name\":\"job\",\"durationSeconds\":%d,\"cpuUtilPct\":%.1f}\\n' \"\$DURATION\" \"\$AVG_UTIL\" > '${work}/steps.jsonl'
                    '${work}/cienergy-aggregator' \\
                      --start  \"\$START\" \\
                      --end    \"\$(date -u +%FT%TZ)\" \\
                      --platform jenkins \\
                      --repo   \"\${GIT_URL:-${env.JOB_NAME}}\" \\
                      --workflow \"${env.JOB_NAME}\" \\
                      --ref    \"\${GIT_BRANCH:-}\" \\
                      --commit \"\${GIT_COMMIT:-0000000}\" \\
                      --run-id \"${env.BUILD_NUMBER}\" \\
                      --os     \"Linux\" --arch \"x86_64\" \\
                      --cpu-model '${cfg.cpuModel}' \\
                      --tdp    ${cfg.cpuTdpW} \\
                      --region '${cfg.region}' \\
                      --steps-file '${work}/steps.jsonl' \\
                      --out    '${work}/energy-report.json'
                """
            }
            if (cfg.electricityMapsTokenCred) {
                withCredentials([string(credentialsId: cfg.electricityMapsTokenCred, variable: 'CIENERGY_EMAPS_TOKEN')]) {
                    aggregateClosure()
                }
            } else {
                aggregateClosure()
            }
        }

        archiveArtifacts artifacts: '.cienergy/energy-report.json', allowEmptyArchive: true, fingerprint: true

        // ----- Optional ingester push --------------------------------------
        if (cfg.ingesterUrl) {
            def pushClosure = {
                sh """
                    set -euo pipefail
                    URL='${cfg.ingesterUrl}'
                    AUTH=()
                    [ -n "\${CIENERGY_INGESTER_TOKEN:-}" ] && AUTH=(-H "Authorization: Bearer \$CIENERGY_INGESTER_TOKEN")
                    for i in 1 2 3; do
                      if curl -fsS -X POST "\${URL%/}/v1/runs" \\
                           -H 'Content-Type: application/json' "\${AUTH[@]}" \\
                           --data @'${work}/energy-report.json' -o /tmp/cienergy-resp.json; then
                        echo "cienergy: pushed to \$URL"; exit 0
                      fi
                      echo "cienergy: push failed (\$i)"; sleep \$((i*i))
                    done
                    echo "cienergy: gave up"
                """
            }
            if (cfg.ingesterTokenCred) {
                withCredentials([string(credentialsId: cfg.ingesterTokenCred, variable: 'CIENERGY_INGESTER_TOKEN')]) {
                    pushClosure()
                }
            } else {
                pushClosure()
            }
        }
    }
}

