docker run -d \
  --name openfoundry-test-mysql \
  -e MYSQL_ROOT_PASSWORD=rootpass \
  -e MYSQL_DATABASE=testdb \
  --tmpfs /var/lib/mysql:rw,noexec,nosuid,size=1024m \
  -p 3306:3306 \
  mysql:8.4.11
