
# SCRIPTS NOTES

## CURL commands

```bash
curl -i -X POST localhost:8080/users \
  -H 'Content-Type: application/json' \
  -d '{"name":"Seed Password Donor","email":"donor@example.com","password":"secret123"}'


  curl -i -X POST localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"donor@example.com","password":"secret123"}'
```