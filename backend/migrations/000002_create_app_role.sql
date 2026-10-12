-- The official PostgreSQL image runs this script with psql on an empty data volume.
-- Read credentials from the container environment instead of storing passwords in SQL.
\getenv app_db_user APP_DB_USER
\getenv app_db_password APP_DB_PASSWORD
\getenv bootstrap_user POSTGRES_USER
\getenv app_db_name POSTGRES_DB

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'app_db_user', :'app_db_password')
\gexec

GRANT CONNECT ON DATABASE :"app_db_name" TO :"app_db_user";
GRANT USAGE ON SCHEMA public TO :"app_db_user";
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO :"app_db_user";
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO :"app_db_user";

ALTER DEFAULT PRIVILEGES FOR ROLE :"bootstrap_user" IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO :"app_db_user";
ALTER DEFAULT PRIVILEGES FOR ROLE :"bootstrap_user" IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO :"app_db_user";
