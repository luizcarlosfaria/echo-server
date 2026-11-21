// Package store mantém o histórico em memória dos últimos N requests (buffer circular).
package store

import (
	"sync"

	"github.com/luizcarlosfaria/echo-server/internal/model"
)

// Tipos de evento.
const (
	EventRequest = "request"
	EventCleared = "cleared"
)

// Event é publicado para os assinantes a cada request armazenado ou limpeza do histórico.
type Event struct {
	Type          string
	Request       *model.Request
	TotalReceived int64
}

// Store é o histórico. Ao atingir a capacidade, cada novo request sobrescreve o mais antigo.
type Store struct {
	mu          sync.Mutex
	buffer      []*model.Request
	head        int // posição do mais antigo
	count       int
	lastID      int64
	subscribers map[int]chan Event
	nextSubID   int
}

// New cria um histórico com a capacidade informada (> 0).
func New(capacity int) *Store {
	return &Store{
		buffer:      make([]*model.Request, capacity),
		subscribers: map[int]chan Event{},
	}
}

// Capacity é a quantidade máxima de requests mantidos.
func (s *Store) Capacity() int { return len(s.buffer) }

// Count é a quantidade de requests armazenados agora.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// TotalReceived é o total de requests recebidos desde a subida (inclui os já expurgados).
func (s *Store) TotalReceived() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastID
}

// Add armazena uma cópia do request com um Id sequencial e notifica os assinantes.
func (s *Store) Add(request model.Request) *model.Request {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastID++
	request.ID = s.lastID
	stored := &request

	capacity := len(s.buffer)
	index := (s.head + s.count) % capacity
	if s.count < capacity {
		s.count++
	} else {
		s.head = (s.head + 1) % capacity // expurga o mais antigo
	}
	s.buffer[index] = stored

	// Publicar dentro do lock garante a ordem dos eventos; publish nunca bloqueia.
	s.publish(Event{Type: EventRequest, Request: stored, TotalReceived: s.lastID})
	return stored
}

// Snapshot devolve os requests armazenados, do mais recente para o mais antigo.
// limit <= 0 devolve todos.
func (s *Store) Snapshot(limit int) []*model.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked(limit)
}

// Get devolve o request pelo Id, ou nil se ele já foi expurgado.
func (s *Store) Get(id int64) *model.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.count == 0 {
		return nil
	}
	// Ids são contíguos dentro do buffer: acesso O(1).
	offset := id - s.buffer[s.head].ID
	if offset < 0 || offset >= int64(s.count) {
		return nil
	}
	return s.buffer[(s.head+int(offset))%len(s.buffer)]
}

// Clear remove todos os requests (o contador de Ids continua).
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.buffer)
	s.head = 0
	s.count = 0
	s.publish(Event{Type: EventCleared, TotalReceived: s.lastID})
}

// Subscribe registra um assinante e devolve, atomicamente, o snapshot atual: nenhum request
// fica de fora nem aparece duplicado entre o snapshot e os eventos seguintes.
// O canal guarda até queueSize eventos; quando enche, os mais antigos são descartados.
func (s *Store) Subscribe(queueSize int) (snapshot []*model.Request, totalReceived int64, events <-chan Event, unsubscribe func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan Event, queueSize)
	id := s.nextSubID
	s.nextSubID++
	s.subscribers[id] = ch

	var once sync.Once
	unsubscribe = func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.subscribers, id)
		})
	}
	return s.snapshotLocked(0), s.lastID, ch, unsubscribe
}

func (s *Store) snapshotLocked(limit int) []*model.Request {
	take := s.count
	if limit > 0 && limit < take {
		take = limit
	}
	result := make([]*model.Request, take)
	for i := range take {
		result[i] = s.buffer[(s.head+s.count-1-i)%len(s.buffer)]
	}
	return result
}

// publish entrega o evento sem bloquear: um assinante lento perde os eventos mais antigos,
// mas nunca atrasa o echo nem os outros assinantes. Só é chamado com o lock tomado, então
// este é o único escritor de cada canal.
func (s *Store) publish(event Event) {
	for _, ch := range s.subscribers {
		select {
		case ch <- event:
		default:
			select {
			case <-ch: // descarta o mais antigo
			default:
			}
			select {
			case ch <- event:
			default:
			}
		}
	}
}
