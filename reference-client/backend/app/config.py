from pydantic_settings import BaseSettings


class Settings(BaseSettings):
    DATABASE_URL: str = "postgresql+asyncpg://postgres:postgres@localhost:5432/reference_db"
    AXYRA_CONSOLE_URL: str = "http://localhost:5173"

    class Config:
        env_file = ".env"


settings = Settings()

