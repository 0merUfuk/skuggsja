#include <arpa/inet.h>
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <stdbool.h>
#include <stdlib.h>
#include <sys/socket.h>
#include <unistd.h>

typedef int (*connect_function)(int, const struct sockaddr *, socklen_t);
typedef int (*connectx_function)(int, const sa_endpoints_t *, sae_associd_t,
                                unsigned int, const struct iovec *,
                                unsigned int, size_t *, sae_connid_t *);
typedef ssize_t (*sendmsg_function)(int, const struct msghdr *, int);
typedef ssize_t (*sendto_function)(int, const void *, size_t, int,
                                  const struct sockaddr *, socklen_t);

static int attempt_log = -1;

__attribute__((constructor)) static void open_attempt_log(void) {
  const char *path = getenv("SKUGGSJA_NETWORK_ATTEMPT_LOG");
  if (path != NULL && path[0] != '\0') {
    attempt_log = open(path, O_WRONLY | O_APPEND | O_CLOEXEC);
    if (attempt_log >= 0) {
      static const char active[] = "network-guard-active\n";
      (void)write(attempt_log, active, sizeof(active) - 1);
    }
  }
}

static bool is_loopback_or_local(const struct sockaddr *address) {
  if (address == NULL) {
    return false;
  }
  if (address->sa_family == AF_UNIX) {
    return true;
  }
  if (address->sa_family == AF_INET) {
    const struct sockaddr_in *ipv4 = (const struct sockaddr_in *)address;
    return (ntohl(ipv4->sin_addr.s_addr) >> 24) == 127;
  }
  if (address->sa_family == AF_INET6) {
    const struct sockaddr_in6 *ipv6 = (const struct sockaddr_in6 *)address;
    return IN6_IS_ADDR_LOOPBACK(&ipv6->sin6_addr);
  }
  return false;
}

static void record_external_attempt(void) {
  static const char message[] = "external-connect-attempt\n";
  if (attempt_log >= 0) {
    (void)write(attempt_log, message, sizeof(message) - 1);
  }
}

static int guarded_connect(int socket_fd, const struct sockaddr *address,
                           socklen_t address_length) {
  static connect_function real_connect = NULL;
  if (!is_loopback_or_local(address)) {
    record_external_attempt();
    errno = EPERM;
    return -1;
  }
  if (real_connect == NULL) {
    real_connect = (connect_function)dlsym(RTLD_NEXT, "connect");
  }
  if (real_connect == NULL) {
    errno = ENOSYS;
    return -1;
  }
  return real_connect(socket_fd, address, address_length);
}

static int guarded_connectx(int socket_fd, const sa_endpoints_t *endpoints,
                            sae_associd_t association_id, unsigned int flags,
                            const struct iovec *iov, unsigned int iov_count,
                            size_t *bytes_written,
                            sae_connid_t *connection_id) {
  static connectx_function real_connectx = NULL;
  if (endpoints == NULL || !is_loopback_or_local(endpoints->sae_dstaddr)) {
    record_external_attempt();
    errno = EPERM;
    return -1;
  }
  if (real_connectx == NULL) {
    real_connectx = (connectx_function)dlsym(RTLD_NEXT, "connectx");
  }
  if (real_connectx == NULL) {
    errno = ENOSYS;
    return -1;
  }
  return real_connectx(socket_fd, endpoints, association_id, flags, iov,
                       iov_count, bytes_written, connection_id);
}

static ssize_t guarded_sendto(int socket_fd, const void *buffer, size_t length,
                              int flags, const struct sockaddr *destination,
                              socklen_t destination_length) {
  static sendto_function real_sendto = NULL;
  if (destination != NULL && !is_loopback_or_local(destination)) {
    record_external_attempt();
    errno = EPERM;
    return -1;
  }
  if (real_sendto == NULL) {
    real_sendto = (sendto_function)dlsym(RTLD_NEXT, "sendto");
  }
  if (real_sendto == NULL) {
    errno = ENOSYS;
    return -1;
  }
  return real_sendto(socket_fd, buffer, length, flags, destination,
                     destination_length);
}

static ssize_t guarded_sendmsg(int socket_fd, const struct msghdr *message,
                               int flags) {
  static sendmsg_function real_sendmsg = NULL;
  if (message != NULL && message->msg_name != NULL &&
      !is_loopback_or_local((const struct sockaddr *)message->msg_name)) {
    record_external_attempt();
    errno = EPERM;
    return -1;
  }
  if (real_sendmsg == NULL) {
    real_sendmsg = (sendmsg_function)dlsym(RTLD_NEXT, "sendmsg");
  }
  if (real_sendmsg == NULL) {
    errno = ENOSYS;
    return -1;
  }
  return real_sendmsg(socket_fd, message, flags);
}

#define DYLD_INTERPOSE(replacement, replacee)                                  \
  __attribute__((used)) static struct {                                       \
    const void *replacement;                                                  \
    const void *replacee;                                                     \
  } _interpose_##replacee __attribute__((section("__DATA,__interpose"))) = {  \
      (const void *)(unsigned long)&replacement,                              \
      (const void *)(unsigned long)&replacee};

DYLD_INTERPOSE(guarded_connect, connect)
DYLD_INTERPOSE(guarded_connectx, connectx)
DYLD_INTERPOSE(guarded_sendto, sendto)
DYLD_INTERPOSE(guarded_sendmsg, sendmsg)
