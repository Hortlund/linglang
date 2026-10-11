defmodule BenchReference do
  import Bitwise

  def arithmetic do
    4_999_950_000 = arithmetic(0, 0)
    :ok
  end

  defp arithmetic(100_000, total), do: total
  defp arithmetic(i, total), do: arithmetic(i64(i + 1), i64(total + i))

  def calls do
    4_999_950_000 = calls(0, 0)
    :ok
  end

  defp calls(100_000, total), do: total
  defp calls(i, total), do: calls(i64(i + 1), add(total, i))
  defp add(a, b), do: i64(a + b)

  def structs do
    %{x: 4_999_950_000, y: 100_000} = structs(0, %{x: 0, y: 0})
    :ok
  end

  defp structs(100_000, point), do: point

  defp structs(i, point) do
    next = %{point | x: i64(point.x + i)}
    structs(i64(i + 1), %{next | y: i64(next.y + 1)})
  end

  # Like Erlang, thread the value; Elixir has no general mutable local pointer.
  def pointers do
    %{value: 100_000} = pointers(0, %{value: 0})
    :ok
  end

  defp pointers(100_000, counter), do: counter
  defp pointers(i, counter), do: pointers(i64(i + 1), increment(counter))
  defp increment(counter), do: %{counter | value: i64(counter.value + 1)}

  def lists do
    items = list_build(0, [])
    20_000 = length(items)
    199_990_000 = list_sum(items, 0)
    :ok
  end

  defp list_build(20_000, items), do: items
  defp list_build(i, items), do: list_build(i64(i + 1), [i | items])
  defp list_sum([], total), do: total
  defp list_sum([item | rest], total), do: list_sum(rest, i64(total + item))

  def maps do
    {99_990_000, remaining} = map_read(0, map_build(0, %{}), 0)
    5_000 = map_size(remaining)
    :ok
  end

  defp map_build(10_000, items), do: items
  defp map_build(i, items), do: map_build(i64(i + 1), Map.put(items, i, i64(i * 2)))
  defp map_read(10_000, items, total), do: {total, items}

  defp map_read(i, items, total) do
    {:ok, value} = Map.fetch(items, i)
    next = if rem(i, 2) == 0, do: Map.delete(items, i), else: items
    map_read(i64(i + 1), next, i64(total + value))
  end

  def strings do
    1_800_000 = string_lines(0, 0)
    :ok
  end

  defp string_lines(10_000, total), do: total

  defp string_lines(i, total) do
    fields = :binary.split(" 12, 34, 56, 78 ", ",", [:global])

    next =
      Enum.reduce(fields, total, fn field, sum ->
        i64(sum + :erlang.binary_to_integer(:string.trim(field, :both, ~c" \t\r\n")))
      end)

    string_lines(i64(i + 1), next)
  end

  def messages do
    parent = self()
    {pid, ref} = spawn_monitor(fn -> echo(parent, 0) end)
    49_995_000 = round_trips(pid, 0, 0)

    receive do
      {:DOWN, ^ref, :process, ^pid, :normal} -> :ok
    after
      5_000 -> error(:worker_timeout)
    end
  end

  defp echo(_, 10_000), do: :ok

  defp echo(parent, i) do
    receive do
      {^parent, value} -> send(parent, {self(), value})
    after
      5_000 -> error(:message_timeout)
    end

    echo(parent, i64(i + 1))
  end

  defp round_trips(_, 10_000, total), do: total

  defp round_trips(pid, i, total) do
    send(pid, {self(), i})

    value =
      receive do
        {^pid, reply} -> reply
      after
        5_000 -> error(:message_timeout)
      end

    round_trips(pid, i64(i + 1), i64(total + value))
  end

  def prime_jobs do
    parent = self()
    workers = for id <- 0..3 do
      {pid, ref} = spawn_monitor(fn -> prime_worker(parent) end)
      send(pid, {:job, id, (rem(id, 4) + 1) * 1_000})
      {pid, ref}
    end
    11_608 = prime_results(0, 4, %{}, 0)
    Enum.each(workers, fn {pid, ref} ->
      receive do
        {:DOWN, ^ref, :process, ^pid, :normal} -> :ok
      after
        5_000 -> error(:worker_timeout)
      end
    end)
    :ok
  end

  defp prime_worker(parent) do
    receive do
      {:job, -1, _} -> :ok
      {:job, id, limit} ->
        send(parent, {:prime_reply, self(), id, count_primes(2, limit, 0)})
        prime_worker(parent)
    after
      5_000 -> error(:message_timeout)
    end
  end

  defp count_primes(n, limit, total) when n > limit, do: total
  defp count_primes(n, limit, total) do
    add = if prime(n, 2), do: 1, else: 0
    count_primes(n + 1, limit, total + add)
  end
  defp prime(n, d) when d * d > n, do: true
  defp prime(n, d), do: rem(n, d) != 0 and prime(n, d + 1)

  defp prime_results(32, _, seen, total) do
    32 = map_size(seen)
    total
  end
  defp prime_results(completed, next, seen, total) do
    receive do
      {:prime_reply, pid, id, count} ->
        true = id >= 0 and id < 32
        false = Map.has_key?(seen, id)
        ^count = elem({168, 303, 430, 550}, rem(id, 4))
        more = if next < 32 do
          send(pid, {:job, next, (rem(next, 4) + 1) * 1_000})
          next + 1
        else
          send(pid, {:job, -1, 0})
          next
        end
        prime_results(completed + 1, more, Map.put(seen, id, true), total + count)
    after
      5_000 -> error(:message_timeout)
    end
  end

  defp error(reason), do: :erlang.error(reason)

  defp i64(value) do
    bits = value &&& (1 <<< 64) - 1
    if bits >= 1 <<< 63, do: bits - (1 <<< 64), else: bits
  end
end
