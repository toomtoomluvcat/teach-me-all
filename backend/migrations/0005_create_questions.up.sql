CREATE TABLE questions IF NOT EXISTS (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(100),

    exam_id UUID,
    FOREIGN KEY exam_id REFERENCES exams(id)
)
