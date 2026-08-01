CREATE TABLE choices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    content VARCHAR(100),
    is_correct BOOLEAN,

    question_id UUID,
    FOREIGN KEY (question_id) REFERENCES questions(id) ON DELETE CASCADE
)
