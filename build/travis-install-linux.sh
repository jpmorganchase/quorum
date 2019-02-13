#!/bin/bash
set -e
# install geth and dependencies for acceptance tests
echo "---> install started ..."
echo "---> building geth ..."
sudo modprobe fuse
sudo chmod 666 /dev/fuse
sudo chown root:${USER} /etc/fuse.conf
go run build/ci.go install
echo "---> building geth done"

echo "---> installing tools ..."
sudo add-apt-repository -y ppa:ethereum/ethereum
sudo add-apt-repository -y ppa:openjdk-r/ppa
sudo apt update
sudo apt-get -y install solc openjdk-8-jre-headless
java -version
mvn --version
solc --version
echo "---> tools installation done"

echo "---> cloning quorum-cloud and quorum-acceptance-tests ..."
git clone https://github.com/jpmorganchase/quorum-acceptance-tests.git ${TRAVIS_HOME}/quorum-acceptance-tests
git clone https://github.com/jpmorganchase/quorum-cloud.git ${TRAVIS_HOME}/quorum-cloud
echo "---> cloning done"

echo "---> getting tessera jar ..."
wget https://github.com/jpmorganchase/tessera/releases/download/tessera-0.8/tessera-app-0.8-app.jar -O $HOME/tessera.jar -q
echo "---> tessera done"

echo "---> getting gauge jar ..."
wget https://github.com/getgauge/gauge/releases/download/v1.0.4/gauge-1.0.4-linux.x86_64.zip -O gauge.zip -q
sudo unzip -o gauge.zip -d /usr/local/bin
gauge telemetry off
cd ${TRAVIS_HOME}/quorum-acceptance-tests
gauge install
echo "---> gauge installation done"

echo "---> install done"