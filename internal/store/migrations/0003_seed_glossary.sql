-- SPDX-License-Identifier: Apache-2.0
-- Seed glossary of common tech terms (AI-7), created once with the
-- database. The operator can edit or delete it like any other glossary; a
-- deleted seed doesn't come back. Terms are written as spoken in the
-- source language, with the preferred translation into the other one.

INSERT INTO glossaries (id, name, data)
SELECT 'tech-terms', 'Tech terms (EN/ES)', json_object(
    'id', 'tech-terms',
    'name', 'Tech terms (EN/ES)',
    'updatedAt', strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
    'terms', json_array(
        json_object('term', 'machine learning', 'translations', json_object('es', 'aprendizaje automático')),
        json_object('term', 'artificial intelligence', 'translations', json_object('es', 'inteligencia artificial')),
        json_object('term', 'large language model', 'translations', json_object('es', 'modelo de lenguaje grande')),
        json_object('term', 'open source', 'translations', json_object('es', 'código abierto')),
        json_object('term', 'cloud', 'translations', json_object('es', 'nube'), 'note', 'Cloud computing'),
        json_object('term', 'container', 'translations', json_object('es', 'contenedor')),
        json_object('term', 'cluster', 'translations', json_object('es', 'clúster')),
        json_object('term', 'deployment', 'translations', json_object('es', 'despliegue')),
        json_object('term', 'database', 'translations', json_object('es', 'base de datos')),
        json_object('term', 'observability', 'translations', json_object('es', 'observabilidad')),
        json_object('term', 'framework', 'translations', json_object('es', 'framework'), 'note', 'Spanish-speaking developers say framework'),
        json_object('term', 'pull request', 'translations', json_object('es', 'pull request')),
        json_object('term', 'backend', 'translations', json_object('es', 'backend')),
        json_object('term', 'frontend', 'translations', json_object('es', 'frontend')),
        json_object('term', 'aprendizaje automático', 'translations', json_object('en', 'machine learning')),
        json_object('term', 'inteligencia artificial', 'translations', json_object('en', 'artificial intelligence')),
        json_object('term', 'código abierto', 'translations', json_object('en', 'open source')),
        json_object('term', 'nube', 'translations', json_object('en', 'cloud'), 'note', 'Cloud computing, not the weather'),
        json_object('term', 'despliegue', 'translations', json_object('en', 'deployment')),
        json_object('term', 'base de datos', 'translations', json_object('en', 'database'))
    ),
    'doNotTranslate', json_array(
        'Kubernetes', 'Docker', 'React', 'TypeScript', 'JavaScript', 'Python',
        'Node.js', 'GitHub', 'GitLab', 'Linux', 'AWS', 'Azure', 'Google Cloud',
        'PostgreSQL', 'Terraform', 'Prometheus', 'Grafana', 'OpenTelemetry',
        'API', 'DevOps', 'Nerdearla'
    )
)
WHERE NOT EXISTS (SELECT 1 FROM glossaries WHERE id = 'tech-terms');
