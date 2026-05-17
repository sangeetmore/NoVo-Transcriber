from pydantic import AliasChoices, Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    video_db_api_key: str = Field(alias="VIDEO_DB_API_KEY")
    video_db_base_url: str = Field(default="https://api.videodb.io", alias="VIDEO_DB_BASE_URL")

    notion_token: str = Field(alias="NOTION_TOKEN")
    notion_parent_page_id: str = Field(alias="NOTION_PARENT_PAGE_ID")
    notion_version: str = Field(default="2022-06-28", alias="NOTION_VERSION")

    llm_api_key: str = Field(
        default="",
        validation_alias=AliasChoices("OPENAI_API_KEY", "LLM_API_KEY"),
    )
    llm_base_url: str = Field(
        default="",
        validation_alias=AliasChoices("OPENAI_BASE_URL", "LLM_BASE_URL"),
    )

    # Default to OpenAI models until sandbox Qwen path is wired.
    curator_model: str = Field(default="gpt-4.1-mini", alias="CURATOR_MODEL")
    curator_fallback_model: str = Field(default="gpt-4o-mini", alias="CURATOR_FALLBACK_MODEL")
    tool_planner_model: str = Field(default="google/gemma-4-E2B-it", alias="TOOL_PLANNER_MODEL")

    sandbox_tier: str = Field(default="small", alias="SANDBOX_TIER")
    sandbox_idle_timeout: int = Field(default=900, alias="SANDBOX_IDLE_TIMEOUT")
    capture_client_token_ttl_seconds: int = Field(default=1800, alias="CAPTURE_CLIENT_TOKEN_TTL_SECONDS")
    use_sandbox: bool = Field(default=False, alias="USE_SANDBOX")
    visual_index_model: str = Field(default="google/gemma-4-E2B-it", alias="VISUAL_INDEX_MODEL")
    visual_index_prompts: str = Field(default="", alias="VISUAL_INDEX_PROMPTS")
    audio_index_model: str = Field(default="Qwen/Qwen3.5-9B", alias="AUDIO_INDEX_MODEL")

    backend_host: str = Field(default="127.0.0.1", alias="BACKEND_HOST")
    backend_port: int = Field(default=8000, alias="BACKEND_PORT")
    curator_initial_window_seconds: int = Field(default=12, alias="CURATOR_INITIAL_WINDOW_SECONDS")
    curator_window_seconds: int = Field(default=30, alias="CURATOR_WINDOW_SECONDS")
    notion_batch_interval_s: float = Field(default=4.0, alias="NOTION_BATCH_INTERVAL_S")
    screenshot_local_fallback: bool = Field(default=True, alias="SCREENSHOT_LOCAL_FALLBACK")
    screenshot_local_first: bool = Field(default=False, alias="SCREENSHOT_LOCAL_FIRST")

    model_config = SettingsConfigDict(
        env_file="../.env",
        env_file_encoding="utf-8",
        extra="ignore",
    )


settings = Settings()
